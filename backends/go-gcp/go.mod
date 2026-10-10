module github.com/ram290476/hybrid-cloud-dashboard/backends/go-gcp

go 1.22

require (
	github.com/ram290476/hybrid-cloud-dashboard/libs/devjwt v0.0.0
	github.com/ram290476/hybrid-cloud-dashboard/libs/merge v0.0.0
)

replace github.com/ram290476/hybrid-cloud-dashboard/libs/devjwt => ../../libs/devjwt

replace github.com/ram290476/hybrid-cloud-dashboard/libs/merge => ../../libs/merge
