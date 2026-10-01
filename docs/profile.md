# Profiles

A profile stores a meshStack endpoint, the credential to call it with, and a default workspace.
`meshstack login` creates one, and `meshstack profile` manages them all.

![meshstack profile starts with no profile, adds two, makes one the current profile and deletes the other](profile.gif)

## On a terminal

`meshstack profile` shows a table of the profiles. 🏠 marks the current profile, the one a command
uses unless `--profile` or `MESHSTACK_PROFILE` names another.

| Key | Action |
| --- | --- |
| ↑ / ↓ | Select a profile |
| Enter | Edit the selected profile |
| a | Add a profile |
| d | Delete the selected profile, after you confirm in a dialog |
| u | Make the selected profile the current one |
| q / Esc | Quit |

The delete dialog focuses Keep first. Press y, or select Delete and press Enter, to delete the
profile together with its stored credentials. Esc keeps it.

The form that adds or edits a profile asks for these fields:

- **Endpoint**: the meshStack API. It suggests the endpoints of your profiles and
  `http://localhost:8080`.
- **Default workspace**: optional. It suggests the workspaces that you can list with the stored
  credential of a profile at the same endpoint.
- **Name**: an empty name takes the suggested one, made from the host of the endpoint.

Tab completes a suggestion, ↑ / ↓ picks another one, Enter goes to the next field, and Esc cancels
the form. A new endpoint removes the stored credentials of the profile, and a new name takes them
along.

## In scripts

These subcommands also work without a terminal:

| Command | What it does |
| --- | --- |
| `meshstack profile list` | Lists the profiles. `-o json` writes JSON. |
| `meshstack profile show` | Shows a profile and the status of its stored credential. `-o json` writes JSON. |
| `meshstack profile add` | Adds a profile. `--profile`, `--endpoint` and `--workspace` give the default answers. |
| `meshstack profile edit` | Edits the profile that `--profile` names. An empty answer keeps the value. |
| `meshstack profile delete` | Deletes the profile that `--profile` names, with its stored credentials. `--yes` skips the question. |

Without a terminal, `add` and `edit` ask one question per line on standard input. Without
`--profile`, `show` uses the current profile, and on a terminal `show`, `edit` and `delete` ask which
profile to use.

## Re-record the gif

See [`demo/README.md`](demo/README.md).
