# This module puts the repository's GitHub configuration under version control. It is deployed by
# hand, like the equivalent modules in meshstack-hub, meshfed-release and terraform-provider-meshstack.
locals {
  github_repository_name = "meshstack-cli"

  # Every check that gates a merge, mapped to the app allowed to report it: a check that is not
  # listed here cannot block a merge, and pinning the app stops anything else reporting under the
  # same name.
  #
  # Apply this only once every check named here can actually report, and merge right after: in
  # between, a pull request off the old default branch requires a check that nothing reports.
  required_checks = {
    "Go Build"                             = local.github_actions_app_id
    "Go Lint and Format Check"             = local.github_actions_app_id
    "Go Test"                              = local.github_actions_app_id
    "Acceptance Tests (meshStack backend)" = local.satellite_app_id
  }

  # The provider cannot resolve an app slug to an id. Read one off a commit that carries the check:
  #   gh api /repos/meshcloud/meshstack-cli/commits/<sha>/check-runs \
  #     --jq '.check_runs[] | {name, app_id: .app.id, app: .app.slug}'
  github_actions_app_id = 15368  # github-actions
  satellite_app_id      = 781479 # meshcloud-gh-actions, which meshfed-release reports with
}

resource "github_repository_ruleset" "protect_default_branch" {
  repository  = local.github_repository_name
  name        = "Protect default branch"
  target      = "branch"
  enforcement = "active"

  bypass_actors {
    actor_id    = 0 # org admin (role-based; GitHub stores 0)
    actor_type  = "OrganizationAdmin"
    bypass_mode = "always"
  }

  bypass_actors {
    actor_id    = 2 # maintain
    actor_type  = "RepositoryRole"
    bypass_mode = "always"
  }

  conditions {
    ref_name {
      include = ["~DEFAULT_BRANCH"]
      exclude = []
    }
  }

  rules {
    deletion         = true
    non_fast_forward = true # force push

    # Rebase-only and a linear history, which is what the other meshcloud repositories enforce, are
    # both impossible here: client/ arrives as a git subtree, and every `git subtree pull` produces
    # a merge commit that only a merge can land on the default branch.
    required_linear_history = false

    pull_request {
      required_approving_review_count   = 1
      required_review_thread_resolution = true
      allowed_merge_methods             = ["rebase", "merge"]
      dismiss_stale_reviews_on_push     = false
      require_code_owner_review         = false
      require_last_push_approval        = false
    }

    required_status_checks {
      # A pull request has to be up to date with the default branch before it can merge, so a
      # check result always describes the code that actually lands.
      strict_required_status_checks_policy = true

      dynamic "required_check" {
        for_each = local.required_checks
        content {
          context        = required_check.key
          integration_id = required_check.value
        }
      }
    }
  }
}
