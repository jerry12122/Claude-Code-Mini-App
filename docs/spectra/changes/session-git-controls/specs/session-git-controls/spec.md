## Purpose

Lets a signed-in user inspect and perform a small set of Git operations on a session's working directory from the chat screen: view branch, sync state and uncommitted files, switch local branches, fast-forward pull and push to the existing upstream. It exists so a user on a phone can handle routine Git steps without asking the agent to run commands.

## ADDED Requirements

### Requirement: Git status report

The system SHALL expose `GET /sessions/:id/git/status` for signed-in non-guest users. It SHALL report whether the session's `work_dir` is a Git working tree, the current branch (or short hash when HEAD is detached), the upstream branch name, the number of commits ahead of and behind that upstream, and the list of uncommitted files. Each file entry SHALL carry its path and the booleans `staged`, `unstaged`, `untracked` and `conflict`; one file SHALL be able to be both `staged` and `unstaged`.

#### Scenario: Clean branch in sync with upstream

- **WHEN** the working tree has no changes and the branch is level with its upstream
- **THEN** the response has `is_repo=true`, `ahead=0`, `behind=0`, an empty `files` list and `total=0`

#### Scenario: Ahead and behind upstream

- **GIVEN** the local branch has 2 commits not on `origin/main` and `origin/main` has 1 commit not on the local branch
- **WHEN** the status is requested
- **THEN** the response reports `upstream="origin/main"`, `ahead=2` and `behind=1`

#### Scenario: Branch without upstream

- **WHEN** the current branch has no upstream configured
- **THEN** `upstream` is an empty string and `ahead` and `behind` are 0

#### Scenario: Detached HEAD

- **WHEN** HEAD points directly at a commit
- **THEN** `detached=true` and `branch` is the short commit hash

#### Scenario: Work directory is not a Git repository

- **WHEN** the session's `work_dir` exists but is not inside a Git working tree
- **THEN** the response is HTTP 200 with `is_repo=false` and no other Git fields

##### Example: file categories

| Working tree state of one file | staged | unstaged | untracked | conflict |
| --- | --- | --- | --- | --- |
| modified, not added | false | true | false | false |
| modified and added | true | false | false | false |
| added, then modified again | true | true | false | false |
| new file never added | false | false | true | false |
| both sides edited during a merge | false | false | false | true |

#### Scenario: Too many changed files

- **WHEN** more than 200 files are uncommitted
- **THEN** `files` holds 200 entries, `total` holds the full count and `truncated=true`

#### Scenario: Session without a usable work directory

- **WHEN** the session has no `work_dir`, or the directory no longer exists
- **THEN** the request fails with HTTP 400 and an error message

### Requirement: Local branch listing

The system SHALL expose `GET /sessions/:id/git/branches` returning the current branch name and the names of all local branches sorted by name. Remote-only branches SHALL NOT be listed.

#### Scenario: List local branches

- **GIVEN** local branches `main` and `dev`, with `main` checked out
- **WHEN** the branches are requested
- **THEN** the response is `{"current":"main","branches":["dev","main"]}`

### Requirement: Switch to an existing local branch

The system SHALL expose `POST /sessions/:id/git/checkout` with body `{"branch": "<name>"}`. It SHALL switch only to a name that exactly matches an existing local branch, and SHALL NOT create branches, stash changes or force the switch. When Git refuses the switch, the system SHALL return Git's own message.

#### Scenario: Switch succeeds

- **WHEN** the body names an existing local branch and the working tree allows the switch
- **THEN** the response is HTTP 200 with `ok=true` and a following status request reports the new branch

#### Scenario: Unknown or unsafe branch name

- **WHEN** the body names a branch that is not a local branch, is empty, or starts with `-`
- **THEN** the response is HTTP 400 and no Git command that changes the working tree runs

#### Scenario: Uncommitted changes block the switch

- **WHEN** Git refuses the switch because local modifications would be overwritten
- **THEN** the response is HTTP 422, `error` carries Git's message, and the working tree and current branch are unchanged

### Requirement: Fast-forward-only pull

The system SHALL expose `POST /sessions/:id/git/pull` which runs a fast-forward-only pull of the current branch from its upstream. The system SHALL NOT create merge commits or rebase.

#### Scenario: Pull fast-forwards

- **GIVEN** the upstream has 1 commit the local branch lacks and the local branch has no commits of its own
- **WHEN** pull is requested
- **THEN** the response is HTTP 200 with `ok=true` and a following status request reports `behind=0`

#### Scenario: Branches have diverged

- **GIVEN** both local branch and upstream have commits the other lacks
- **WHEN** pull is requested
- **THEN** the response is HTTP 422 with Git's message, and no merge or rebase is started

#### Scenario: No upstream on pull

- **WHEN** pull is requested and the current branch has no upstream, or HEAD is detached
- **THEN** the response is HTTP 422 with a message saying no upstream is set, and no Git network command runs

### Requirement: Push to the existing upstream

The system SHALL expose `POST /sessions/:id/git/push` which pushes only the current branch to its configured upstream. The system SHALL NOT force-push, SHALL NOT set an upstream, and SHALL NOT push any other branch regardless of the user's `push.default` setting.

#### Scenario: Push succeeds

- **GIVEN** the local branch is 1 commit ahead of its upstream
- **WHEN** push is requested
- **THEN** the response is HTTP 200 with `ok=true` and a following status request reports `ahead=0`

#### Scenario: Remote rejects the push

- **WHEN** the remote rejects the push because the upstream has newer commits
- **THEN** the response is HTTP 422 with Git's message and nothing is force-pushed

#### Scenario: No upstream on push

- **WHEN** push is requested and the current branch has no upstream, or HEAD is detached
- **THEN** the response is HTTP 422 with a message saying no upstream is set, and no remote branch is created

#### Scenario: Other local branches are untouched

- **GIVEN** the user's Git config sets `push.default` to `matching` and another local branch also has unpushed commits
- **WHEN** push is requested
- **THEN** only the current branch is pushed

#### Scenario: Push configuration cannot expand scope

- **GIVEN** Git configuration enables `remote.origin.mirror`, `push.followTags` or a force-capable `remote.origin.push` refspec
- **WHEN** push is requested on a branch tracking `origin/main`
- **THEN** only the current HEAD is pushed to `refs/heads/main`, no tag or other branch is updated, and newer remote commits are not overwritten

### Requirement: Write operations are blocked while the working directory is busy

Switch, pull and push SHALL be rejected with HTTP 409 when any session whose `work_dir` resolves to the same absolute path as the target session has a status other than `idle`, or when another switch, pull or push on that path is still running. The status request and the branch listing SHALL NOT be subject to this rule.

#### Scenario: Agent is running in the session

- **WHEN** the target session's status is `running` and a pull is requested
- **THEN** the response is HTTP 409 and no Git command runs

#### Scenario: Another session on the same directory is running

- **GIVEN** sessions A and B share one `work_dir` and B is `awaiting_confirm`
- **WHEN** a push is requested on A
- **THEN** the response is HTTP 409

#### Scenario: Status is still readable while busy

- **WHEN** the session is `running` and the status is requested
- **THEN** the response is HTTP 200 with the current Git state

#### Scenario: Concurrent write operations

- **WHEN** a pull is in progress on a path and a second write operation is requested on the same path
- **THEN** the second request is rejected with HTTP 409 without waiting

### Requirement: Non-interactive and time-bounded Git execution

Every Git command run by this capability SHALL run without prompting for input, SHALL use a fixed argument list built by the server (user text appears only as an already-validated branch name), and SHALL be stopped when its time limit is reached: 10 seconds for status and branch listing, 30 seconds for a switch, 90 seconds for pull and push. A timeout SHALL be reported as HTTP 504 and a command-line failure as HTTP 422.

#### Scenario: Remote needs credentials that are not stored

- **WHEN** a push needs a username or password that the host's Git configuration cannot supply
- **THEN** the command fails immediately with HTTP 422 and Git's message instead of waiting for input

#### Scenario: Command exceeds its limit

- **WHEN** a pull has not finished after 90 seconds
- **THEN** the request returns HTTP 504 stating the time limit

#### Scenario: Git is not installed

- **WHEN** no `git` executable is found on the server's PATH
- **THEN** the request returns HTTP 500 saying Git must be installed on the server

### Requirement: Access restricted to signed-in non-guest users

The Git routes SHALL require the same sign-in as other session routes. Share-link guests SHALL be rejected for every Git route, including the read-only ones.

#### Scenario: Guest requests status

- **WHEN** a guest of a share link requests `GET /sessions/:id/git/status` for the shared session
- **THEN** the response is HTTP 403

#### Scenario: Unauthenticated request

- **WHEN** a request carries no valid sign-in
- **THEN** the response is the standard unauthorized response and no Git command runs

### Requirement: Inline Git controls

For a signed-in non-guest user, the chat header SHALL show a branch button reusing the existing GitBranchBadge, revealing a dark branch button group with the current branch marked, a badge with `↑N ↓N` and no visible words for ahead or behind, a badge with green `+N` additions and red `-N` deletions, and a separate Git button. Both branch and Git popovers SHALL reuse the application menu colors and item styling. The Git button SHALL reveal a non-modal popover containing Pull and Push buttons. The UI SHALL NOT render the previous Git panel dialog, file list or local-upstream-state explanation. Guests SHALL see only the existing plain branch badge and SHALL NOT query Git routes.

#### Scenario: Header shows compact badges

- **GIVEN** branch `main`, ahead 1, behind 0, additions 2043 and deletions 3
- **WHEN** the chat header is rendered
- **THEN** badges show `main`, `↑1 ↓0`, green `+2043` and red `-3`, and no Git dialog is present

#### Scenario: Select a local branch

- **WHEN** the user opens the branch selector showing `main` and chooses `dev`
- **THEN** checkout is requested for `dev`, and a successful refresh displays `dev`

#### Scenario: Git action popover

- **WHEN** the user activates the separate Git button
- **THEN** a non-modal Pull/Push button group opens next to that button, stays within a narrow viewport, and supports native outside-click and Escape dismissal

#### Scenario: Operation succeeds

- **WHEN** Pull succeeds
- **THEN** the status badges refresh and the controls become usable again

#### Scenario: Operation fails

- **WHEN** a branch switch or Push is rejected
- **THEN** the header displays Git's rejection message and retains the previous branch and badges

#### Scenario: Session is busy

- **WHEN** the session is not idle or the live state is not IDLE
- **THEN** branch switching, Pull and Push are disabled with an accessible reason

#### Scenario: Guest header

- **WHEN** a share guest opens the chat
- **THEN** only the plain branch badge is shown, without Git action controls or Git API queries

### Requirement: Uncommitted line counts

The status response SHALL include nonnegative `additions` and `deletions` counting tracked text changes relative to HEAD across the index and working tree without double-counting. An unborn repository SHALL use the empty tree as its baseline. Binary and untracked files SHALL NOT contribute text line counts. Query failures SHALL use the existing Git error response.

#### Scenario: Combined staged and unstaged changes

- **GIVEN** HEAD has `one\ntwo\n`, the index has `one\nstaged\ntwo\n` and the working file has `one\nfinal\ntwo\n`
- **WHEN** line counts are requested
- **THEN** additions is 1 and deletions is 0

#### Scenario: New repository and unusual paths

- **WHEN** an unborn repository contains a staged two-line text file with whitespace in its name
- **THEN** additions is 2 and deletions is 0

#### Scenario: Binary and untracked files

- **WHEN** only a tracked binary file and an untracked text file have changed
- **THEN** additions and deletions are both 0

### Requirement: Branch lookup cache is invalidated by a switch

After a successful switch, the system SHALL discard the cached branch name for that working directory so that `git_branch` in the session list reflects the new branch immediately.

#### Scenario: Session list right after a switch

- **WHEN** a switch to `dev` succeeds and `/sessions` is requested within 15 seconds
- **THEN** that session's `git_branch` is `dev`
