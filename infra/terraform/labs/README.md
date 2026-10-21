# Optional lab modules

These roots are Terraform configurations. They are not on the default path.
Decision D-native keeps managed Kubernetes (EKS, AKS, GKE) out of the cloud
backends. `make terraform-validate` still checks the HCL with HashiCorp Terraform
(`required_version >= 1.6.0`, CI pins 1.9.8) so the labs stay syntactically honest.
Do not apply them for the portfolio deploy.
