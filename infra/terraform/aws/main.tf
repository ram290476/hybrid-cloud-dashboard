data "aws_iam_policy_document" "ecs_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "task" {
  name               = "hcd-python-task"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

resource "aws_ecs_cluster" "hcd" {
  name = "hcd"
}

resource "aws_ecs_task_definition" "python" {
  family                   = "py-api"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = "256"
  memory                   = "512"
  execution_role_arn       = aws_iam_role.task.arn
  task_role_arn            = aws_iam_role.task.arn

  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "ARM64"
  }

  container_definitions = jsonencode([
    {
      name      = "api"
      image     = var.python_image
      essential = true
      portMappings = [{ containerPort = 8080, protocol = "tcp" }]
      environment = [
        { name = "APP_BACKEND", value = "aws-python" },
        { name = "APP_LANG", value = "python" },
        { name = "APP_RUNTIME", value = "ecs-fargate" },
        { name = "PORT", value = "8080" }
      ]
    },
    {
      name      = "cloudflared"
      image     = "cloudflare/cloudflared:2024.10.0"
      essential = false
      command   = ["tunnel", "--no-autoupdate", "run", "--token", "placeholder"]
    }
  ])
}

resource "aws_ecs_service" "blue" {
  name            = "py-api-blue"
  cluster         = aws_ecs_cluster.hcd.id
  task_definition = aws_ecs_task_definition.python.arn
  desired_count   = var.desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = var.subnet_ids
    assign_public_ip = true
  }
}

resource "aws_ecs_service" "green" {
  name            = "py-api-green"
  cluster         = aws_ecs_cluster.hcd.id
  task_definition = aws_ecs_task_definition.python.arn
  desired_count   = var.desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = var.subnet_ids
    assign_public_ip = true
  }
}

resource "aws_s3_bucket" "serving" {
  bucket = var.serving_bucket
}

resource "aws_dynamodb_table" "prefs" {
  name         = "hcd-prefs"
  billing_mode = "PROVISIONED"
  hash_key     = "user_sub"

  read_capacity  = 1
  write_capacity = 1

  attribute {
    name = "user_sub"
    type = "S"
  }
}

resource "aws_cognito_user_pool" "hcd" {
  name = "hcd"
}

resource "aws_cognito_user_pool_client" "spa" {
  name                                 = "hcd-spa"
  user_pool_id                         = aws_cognito_user_pool.hcd.id
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["openid", "email"]
  allowed_oauth_flows_user_pool_client = true
  callback_urls                        = [var.callback_url]
  supported_identity_providers         = ["COGNITO"]
  generate_secret                      = false
}

resource "aws_iam_openid_connect_provider" "github" {
  url             = "https://token.actions.githubusercontent.com"
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = ["6938fd4d98bab03faadb97b34396831e3780aea1"]
}

data "aws_iam_policy_document" "github" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }
    condition {
      test     = "StringLike"
      variable = "token.actions.githubusercontent.com:sub"
      values   = ["repo:ram290476/hybrid-cloud-dashboard:*"]
    }
  }
}

resource "aws_iam_role" "github" {
  name               = "hcd-github-actions"
  assume_role_policy = data.aws_iam_policy_document.github.json
}
