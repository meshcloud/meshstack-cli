# The building block that the tfstate tests work on: a block of the noop definition of
# meshstack-hub, in a workspace of its own, which the tf-block-runner applies with meshStack's state
# backend.

terraform {
  required_providers {
    meshstack = {
      source = "meshcloud/meshstack"
      # The first release that reads a profile of the meshStack CLI.
      version = ">= 0.26.0"
    }
  }
}

variable "profile" {
  type = string
}

variable "run_id" {
  type = string
}

variable "workspace_manager" {
  type = string
}

provider "meshstack" {
  profile = var.profile
}

resource "meshstack_workspace" "this" {
  metadata = {
    name = var.run_id
  }
  spec = {
    display_name = "meshStack CLI acceptance ${var.run_id}"
  }
}

resource "meshstack_project" "this" {
  metadata = {
    name               = var.run_id
    owned_by_workspace = meshstack_workspace.this.metadata.name
  }
  spec = {
    display_name = "meshStack CLI acceptance ${var.run_id}"
  }
}

resource "meshstack_workspace_user_binding" "manager" {
  metadata = {
    name = "${var.run_id}-manager"
  }
  role_ref = {
    name = "Workspace Manager"
  }
  target_ref = {
    name = meshstack_workspace.this.metadata.name
  }
  subject = {
    name = var.workspace_manager
  }
}

module "noop" {
  source = "git::https://github.com/meshcloud/meshstack-hub.git//modules/meshstack/noop/e2e?ref=main"
  # test_context reaches a const variable of the noop module, which tofu evaluates at init, so it
  # takes the names from var.run_id rather than from the resources above.
  depends_on = [meshstack_project.this, meshstack_workspace_user_binding.manager]

  test_context = {
    hub_git_ref = "main"
    workspace   = var.run_id
    project     = var.run_id
    run_id      = var.run_id
  }
}
