module github.com/nitrictech/cli

go 1.16

require (
	cloud.google.com/go/compute v1.5.0 // indirect
	cloud.google.com/go/iam v0.2.0 // indirect
	github.com/AlecAivazis/survey/v2 v2.3.2
	github.com/Azure/azure-sdk-for-go v61.6.0+incompatible
	github.com/Azure/go-autorest/autorest/to v0.4.0
	github.com/Microsoft/go-winio v0.5.0 // indirect
	github.com/aws/aws-sdk-go v1.43.7 // indirect
	github.com/davecgh/go-spew v1.1.1
	github.com/docker/cli v20.10.12+incompatible
	github.com/docker/docker v20.10.12+incompatible
	github.com/docker/go-connections v0.4.0
	github.com/envoyproxy/protoc-gen-validate v0.6.3 // indirect
	github.com/fasthttp/router v1.4.6
	github.com/getkin/kin-openapi v0.90.0
	github.com/go-openapi/strfmt v0.21.1 // indirect
	github.com/golang/mock v1.6.0
	github.com/golangci/golangci-lint v1.44.2
	github.com/google/go-cmp v0.5.7
	github.com/google/uuid v1.3.0
	github.com/hashicorp/consul/sdk v0.9.0
	github.com/hashicorp/go-getter v1.5.11
	github.com/imdario/mergo v0.3.12
	github.com/jedib0t/go-pretty v4.3.0+incompatible
	github.com/klauspost/compress v1.14.4 // indirect
	github.com/magiconair/properties v1.8.6 // indirect
	github.com/mgutz/ansi v0.0.0-20200706080929-d51e80ef957d // indirect
	github.com/mitchellh/go-testing-interface v1.14.1 // indirect
	github.com/moby/buildkit v0.9.3 // indirect
	github.com/moby/moby v20.10.12+incompatible
	github.com/nitrictech/boxygen v0.0.1-rc.7.0.20211212231606-62c668408f91
	github.com/nitrictech/nitric v0.15.0
	github.com/pkg/errors v0.9.1
	github.com/pterm/pterm v0.12.37
	github.com/pulumi/pulumi-aws/sdk/v4 v4.37.5
	github.com/pulumi/pulumi-azure-native/sdk v1.60.0
	github.com/pulumi/pulumi-azure/sdk/v4 v4.39.0
	github.com/pulumi/pulumi-azuread/sdk/v5 v5.17.0
	github.com/pulumi/pulumi-docker/sdk/v3 v3.1.0
	github.com/pulumi/pulumi-gcp/sdk/v6 v6.12.0
	github.com/pulumi/pulumi/sdk/v3 v3.25.0
	github.com/robfig/cron/v3 v3.0.1
	github.com/savsgio/gotils v0.0.0-20220201163454-d252f0a44d5b // indirect
	github.com/spf13/afero v1.8.1 // indirect
	github.com/spf13/cobra v1.3.0
	github.com/stretchr/objx v0.3.0 // indirect
	github.com/stretchr/testify v1.7.0
	github.com/ulikunitz/xz v0.5.10 // indirect
	github.com/valyala/fasthttp v1.33.0
	golang.org/x/mod v0.5.1
	golang.org/x/net v0.0.0-20220225172249-27dd8689420f // indirect
	golang.org/x/oauth2 v0.0.0-20220223155221-ee480838109b
	golang.org/x/sys v0.0.0-20220224120231-95c6836cb0e7 // indirect
	google.golang.org/grpc v1.44.0
	gopkg.in/ini.v1 v1.66.4 // indirect
	gopkg.in/mcuadros/go-syslog.v2 v2.3.0
	gopkg.in/yaml.v2 v2.4.0
)

replace (
	github.com/opencontainers/image-spec => github.com/opencontainers/image-spec v1.0.2-0.20211123152302-43a7dee1ec31
	github.com/rootless-containers/rootlesskit => github.com/rootless-containers/rootlesskit v0.14.6
)
