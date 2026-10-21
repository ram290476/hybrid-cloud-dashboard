# Lambda Function URL for the Python API. Called from infra/terraform/aws.
# Container image only (backends/python-aws/Dockerfile.lambda). Zip packaging
# is not used. Do not apply this directory on its own.
# Blue/green is two aliases on one published function. The facade keeps one
# aws-python weight and calls the active alias URL.

variable "image_uri" {
  type = string
}

variable "role_arn" {
  type = string
}

variable "function_name" {
  type    = string
  default = "py-api"
}

variable "memory_size" {
  type    = number
  default = 512
}

variable "timeout" {
  type    = number
  default = 10
}

variable "reserved_concurrent_executions" {
  type        = number
  default     = 0
  description = "Stays 0 so a mistaken apply cannot invoke the function. Raise it before the facade calls a Function URL."
}

variable "blue_version" {
  type        = string
  default     = ""
  description = "Published version for the blue alias. Empty follows the version this apply publishes."
}

variable "green_version" {
  type        = string
  default     = ""
  description = "Published version for the green alias. Empty follows the version this apply publishes."
}

variable "environment" {
  type = map(string)
  default = {
    APP_BACKEND = "aws-python"
    APP_LANG    = "python"
    APP_RUNTIME = "lambda"
  }
}

resource "aws_lambda_function" "python" {
  function_name                  = var.function_name
  package_type                   = "Image"
  image_uri                      = var.image_uri
  role                           = var.role_arn
  architectures                  = ["arm64"]
  memory_size                    = var.memory_size
  timeout                        = var.timeout
  publish                        = true
  reserved_concurrent_executions = var.reserved_concurrent_executions

  environment {
    variables = var.environment
  }
}

locals {
  published = aws_lambda_function.python.version
  alias_versions = {
    blue  = var.blue_version != "" ? var.blue_version : local.published
    green = var.green_version != "" ? var.green_version : local.published
  }
}

resource "aws_lambda_alias" "color" {
  for_each         = local.alias_versions
  name             = each.key
  description      = "In-account ${each.key} color. Facade weights stay on the aws-python environment."
  function_name    = aws_lambda_function.python.function_name
  function_version = each.value
}

resource "aws_lambda_function_url" "color" {
  for_each           = aws_lambda_alias.color
  function_name      = aws_lambda_function.python.function_name
  qualifier          = each.value.name
  authorization_type = "NONE"
}

resource "aws_lambda_permission" "url" {
  for_each               = aws_lambda_alias.color
  statement_id           = "AllowFunctionUrl${title(each.key)}"
  action                 = "lambda:InvokeFunctionUrl"
  function_name          = aws_lambda_function.python.function_name
  qualifier              = each.value.name
  principal              = "*"
  function_url_auth_type = "NONE"
}

resource "aws_lambda_permission" "invoke" {
  for_each      = aws_lambda_alias.color
  statement_id  = "AllowInvoke${title(each.key)}"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.python.function_name
  qualifier     = each.value.name
  principal     = "*"
}
