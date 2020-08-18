package client

import (
	"github.com/golang/mock/gomock"
	"testing"
)

func integrationTest(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
}
