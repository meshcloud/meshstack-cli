# The code that the tf-block-runner runs for the bootstrapped building block. It declares no backend,
# so the runner uses meshStack's state backend; the tfstate tests add one of their own.

variable "input" {
  type    = string
  default = null
}

resource "terraform_data" "noop" {
  input = var.input
}
