variable "region" {
  type    = string
  default = "us-west-2"
}

variable "account_id" {
  type        = string
  default     = "000000000000"
  description = "New dedicated AWS account. Do not use 308639168050."
}

variable "reserved_concurrent_executions" {
  type        = number
  default     = 0
  description = "Stays 0 so a mistaken apply cannot invoke the Lambda function. Raise it before the facade calls a Function URL."
}

variable "blue_version" {
  type        = string
  default     = ""
  description = "Published Lambda version for the blue alias. Empty follows the version this apply publishes."
}

variable "green_version" {
  type        = string
  default     = ""
  description = "Published Lambda version for the green alias. Empty follows the version this apply publishes. Pin one color and move the other to flip."
}

variable "python_image" {
  type        = string
  default     = "000000000000.dkr.ecr.us-west-2.amazonaws.com/hcd/python-aws:dev"
  description = "ARM64 Lambda container image built from backends/python-aws/Dockerfile.lambda."
}

variable "serving_bucket" {
  type    = string
  default = "hcd-serving-placeholder"
}

variable "callback_url" {
  type    = string
  default = "http://localhost/callback"
}
