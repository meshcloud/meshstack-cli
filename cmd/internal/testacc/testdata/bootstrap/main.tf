# The building block that the tfstate tests work on, in a workspace of its own. Its definition runs
# testdata/buildingblock at the commit under test, which the tf-block-runner applies with meshStack's
# state backend.

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

variable "repository_url" {
  type = string
}

variable "git_ref" {
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

resource "meshstack_building_block_definition" "noop" {
  metadata = {
    owned_by_workspace = meshstack_workspace.this.metadata.name
  }
  spec = {
    display_name = "${var.run_id} meshStack CLI noop"
    description  = "Runs testdata/buildingblock of the meshStack CLI acceptance tests."
    target_type  = "WORKSPACE_LEVEL"
  }
  version_spec = {
    draft         = true
    deletion_mode = "DELETE"
    implementation = {
      terraform = {
        repository_url    = var.repository_url
        repository_path   = "cmd/internal/testacc/testdata/buildingblock"
        ref_name          = var.git_ref
        terraform_version = "1.12.5"
      }
    }
  }
}

resource "meshstack_building_block" "noop" {
  depends_on = [meshstack_project.this, meshstack_workspace_user_binding.manager]

  wait_for_completion = true
  spec = {
    building_block_definition_version_ref = meshstack_building_block_definition.noop.version_latest
    display_name                          = "${var.run_id}-noop"
    inputs                                = {}
    target_ref = {
      kind = "meshWorkspace"
      name = meshstack_workspace.this.metadata.name
    }
  }

  lifecycle {
    postcondition {
      condition     = self.status.status == "SUCCEEDED"
      error_message = "The run of building block ${self.metadata.uuid} is ${self.status.status}."
    }
  }
}
