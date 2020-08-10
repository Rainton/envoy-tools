#!/bin/bash

########################
# include the magic
########################
. demo-magic.sh

# hide the evidence
clear

cd ..
# Put your stuff here
# this command is typed and executed
pe "go build"
pe "./csds-client -request_file request.yaml -output_file myconfig.json"
