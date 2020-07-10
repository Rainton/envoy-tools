package client

import (
	"context"
	"crypto/x509"
	"flag"
	"fmt"
	csdspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v2"
	envoy_type_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/metadata"
)

type Flag struct {
	uri         string
	platform    string
	authnMode   string
	apiVersion  string
	RequestFile string
	RequestYaml string
	jwt         string
	configFile  string
}

type Client struct {
	Cc         *grpc.ClientConn
	CsdsClient csdspb.ClientStatusDiscoveryServiceClient

	Nm   []*envoy_type_matcher.NodeMatcher
	Md   metadata.MD
	Info Flag
}

// parse flags to info
func ParseFlags() Flag {
	uriPtr := flag.String("service_uri", "trafficdirector.googleapis.com:443", "the uri of the service to connect to")
	platformPtr := flag.String("cloud_platform", "gcp", "the cloud platform (e.g. gcp, aws,  ...)")
	authnModePtr := flag.String("authn_mode", "auto", "the method to use for authentication (e.g. auto, jwt, ...)")
	apiVersionPtr := flag.String("api_version", "v2", "which xds api major version  to use (e.g. v2, v3 ...)")
	requestFilePtr := flag.String("request_file", "", "yaml file that defines the csds request")
	requestYamlPtr := flag.String("request_yaml", "", "yaml string that defines the csds request")
	jwtPtr := flag.String("jwt_file", "", "path of the -jwt_file")
	configFilePtr := flag.String("file_to_save_config", "", "the file name to save config")

	flag.Parse()

	f := Flag{
		uri:         *uriPtr,
		platform:    *platformPtr,
		authnMode:   *authnModePtr,
		apiVersion:  *apiVersionPtr,
		RequestFile: *requestFilePtr,
		RequestYaml: *requestYamlPtr,
		jwt:         *jwtPtr,
		configFile:  *configFilePtr,
	}

	return f
}

// parse the csds request yaml to nodematcher
func (c *Client) ParseNodeMatcher() error {
	if c.Info.RequestFile == "" && c.Info.RequestYaml == "" {
		return fmt.Errorf("missing request yaml")
	}

	var nodematchers []*envoy_type_matcher.NodeMatcher
	err := ParseYaml(c.Info.RequestFile, c.Info.RequestYaml, &nodematchers)
	if err != nil {
		return fmt.Errorf("%v", err)
	}

	c.Nm = nodematchers
	return nil
}

// connect uri with authentication
func (c *Client) ConnWithAuth() error {
	var scope string
	if c.Info.authnMode == "jwt" {
		if c.Info.jwt == "" {
			return fmt.Errorf("missing jwt file")
		} else {
			if c.Info.platform == "gcp" {
				scope = "https://www.googleapis.com/auth/cloud-platform"
				pool, err := x509.SystemCertPool()
				creds := credentials.NewClientTLSFromCert(pool, "")
				perRPC, err := oauth.NewServiceAccountFromFile(c.Info.jwt, scope)
				if err != nil {
					return fmt.Errorf("%v", err)
				}

				c.Cc, err = grpc.Dial(c.Info.uri, grpc.WithTransportCredentials(creds), grpc.WithPerRPCCredentials(perRPC))
				if err != nil {
					return fmt.Errorf("%v", err)
				} else {
					return nil
				}
			}
		}
	} else if c.Info.authnMode == "auto" {
		if c.Info.platform == "gcp" {
			scope = "https://www.googleapis.com/auth/cloud-platform"
			pool, err := x509.SystemCertPool()
			creds := credentials.NewClientTLSFromCert(pool, "")
			perRPC, err := oauth.NewApplicationDefault(context.Background(), scope) // Application Default Credentials (ADC)
			if err != nil {
				return fmt.Errorf("%v", err)
			}

			// parse GCP project number as header for authentication
			if projectNum := ParseGCPProject(c.Nm); projectNum != "" {
				c.Md = metadata.Pairs("x-goog-user-project", projectNum)
			}

			c.Cc, err = grpc.Dial(c.Info.uri, grpc.WithTransportCredentials(creds), grpc.WithPerRPCCredentials(perRPC))
			if err != nil {
				return fmt.Errorf("connect error: %v", err)
			}
			return nil
		} else {
			return fmt.Errorf("Auto authentication mode for this platform is not supported. Please use jwt_file instead")
		}
	} else {
		return fmt.Errorf("Invalid authn_mode")
	}
	return nil
}

// create a new client
func New() (*Client, error) {
	c := &Client{
		Info: ParseFlags(),
	}
	if c.Info.platform != "gcp" {
		return c, fmt.Errorf("Can not support this platform now")
	}
	if c.Info.apiVersion != "v2" {
		return c, fmt.Errorf("Can not suppoort this api version now")
	}

	if err := c.ParseNodeMatcher(); err != nil {
		return c, err
	}

	if err := c.ConnWithAuth(); err != nil {
		return c, err
	}
	defer c.Cc.Close()

	c.CsdsClient = csdspb.NewClientStatusDiscoveryServiceClient(c.Cc)

	if err := c.Run(); err != nil {
		return c, err
	}
	return c, nil
}

// send request and receive response then post process it
func (c *Client) Run() error {
	var ctx context.Context
	if c.Md != nil {
		ctx = metadata.NewOutgoingContext(context.Background(), c.Md)
	} else {
		ctx = context.Background()
	}

	streamClientStatus, err := c.CsdsClient.StreamClientStatus(ctx)
	if err != nil {
		return fmt.Errorf("stream client status error: %v", err)
	}
	req := &csdspb.ClientStatusRequest{NodeMatchers: c.Nm}
	if err := streamClientStatus.Send(req); err != nil {
		return fmt.Errorf("%v", err)
	}

	resp, err := streamClientStatus.Recv()
	if err != nil {
		return fmt.Errorf("%v", err)
	}

	// post process response
	ParseResponse(resp, c.Info.configFile)

	return nil
}