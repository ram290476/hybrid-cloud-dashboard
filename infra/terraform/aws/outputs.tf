output "function_name" {
  value = module.python_lambda.function_name
}

output "function_version" {
  value = module.python_lambda.function_version
}

output "blue_alias_arn" {
  value = module.python_lambda.blue_alias_arn
}

output "green_alias_arn" {
  value = module.python_lambda.green_alias_arn
}

output "blue_function_url" {
  description = "HTTPS endpoint for the blue alias. The facade aws-python backend uses this URL, or green_function_url, for the active color."
  value       = module.python_lambda.blue_function_url
}

output "green_function_url" {
  description = "HTTPS endpoint for the green alias."
  value       = module.python_lambda.green_function_url
}
