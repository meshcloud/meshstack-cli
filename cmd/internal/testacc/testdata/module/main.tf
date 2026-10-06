# Stands in for the noop module of meshstack-hub, which the bootstrapped building block runs: it
# keeps the one resource of that module, so that a plan against the block's state changes no resource.
# An apply with another input changes the state, as a test of concurrent writes needs.

terraform {
  backend "http" {}
}

variable "input" {
  type    = string
  default = null
}

resource "terraform_data" "noop" {
  input = var.input
}
