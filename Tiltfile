# Local kind loop. `tilt up` rebuilds the three backends, the facade, and the UI.
docker_build(
    "localhost:5001/hcd-go-gcp",
    ".",
    dockerfile = "backends/go-gcp/Dockerfile",
    only = ["backends/go-gcp", "libs", "contract"],
)
docker_build(
    "localhost:5001/hcd-python-aws",
    ".",
    dockerfile = "backends/python-aws/Dockerfile",
    only = ["backends/python-aws", "contract"],
)
docker_build(
    "localhost:5001/hcd-dotnet-azure",
    ".",
    dockerfile = "backends/dotnet-azure/Dockerfile",
    only = ["backends/dotnet-azure", "contract"],
)
docker_build(
    "localhost:5001/hcd-facade",
    ".",
    dockerfile = "facade/Dockerfile",
    only = ["facade", "libs/devjwt"],
)
docker_build(
    "localhost:5001/hcd-frontend",
    ".",
    dockerfile = "frontend/Dockerfile",
    only = ["frontend", "deploy/base/frontend"],
)

k8s_yaml(kustomize("deploy/overlays/local"))
k8s_resource("facade", port_forwards = "8080:8080")
k8s_resource("lgtm", port_forwards = "3000:3000", extra_pod_selectors = [{"app": "lgtm"}])
