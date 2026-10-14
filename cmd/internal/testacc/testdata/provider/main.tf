# Reads meshStack with the meshstack provider under exec. v0.24.5 reads no profile of the meshStack
# CLI, only MESHSTACK_ENDPOINT and a credential, so it shows that exec works with any version.

terraform {
  backend "http" {}

  required_providers {
    meshstack = {
      source  = "meshcloud/meshstack"
      version = "0.24.5"
    }
  }
}

variable "workspace" {
  type = string
}

data "meshstack_workspace" "this" {
  metadata = {
    name = var.workspace
  }
}

output "display_name" {
  value = data.meshstack_workspace.this.spec.display_name
}
