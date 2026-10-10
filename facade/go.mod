module github.com/ram290476/hybrid-cloud-dashboard/facade

go 1.22

require (
	github.com/ram290476/hybrid-cloud-dashboard/libs/devjwt v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/ram290476/hybrid-cloud-dashboard/libs/devjwt => ../libs/devjwt
