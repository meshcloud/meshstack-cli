# Profiles

A profile stores a meshStack endpoint, the credential to call it with, and a default workspace.
`meshstack login` creates one, and `meshstack profile` manages them all.

![meshstack profile starts with no profile, adds two, makes one the current profile and deletes it, so that the other becomes the current one](profile.gif)

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

Where you delete the current profile, and one profile is left, that one becomes the current
profile. Where several are left, none is current until you make one the current one, here or with
`meshstack login --profile <name>`. `meshstack profile delete` does the same, and warns of either.

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
`--profile`, `show` shows the profile another command would use, and `edit` and `delete` ask which
profile to change: from a list on a terminal, and by its number otherwise.

`show`, and `edit` before it asks, read the status of the stored credential, for which meshStack
answers about an API key. Where it does not answer within 5s, they show what the profile stores,
and say that the status could not be read.

Where standard input ends before an answer, the question takes its default answer, so the flags can
give every answer:

```shell
meshstack profile add --profile dev --endpoint https://federation.example.com --workspace ws </dev/null
```

It fails, and stores nothing, where a question has no default answer that fits, such as an
endpoint that no flag gives.

## Alongside a login

`meshstack profile` holds the profiles while it is open, and so do `add`, `edit` and `delete` until
they have stored their answers. A `meshstack login` fails within a second meanwhile, rather than
store its profile over what they change. The other way round, they fail while a login runs, which
for a browser login lasts until the browser comes back. `list` and `show` only read, and run
alongside either.

## Re-record the gif

See [`demo/README.md`](demo/README.md).
