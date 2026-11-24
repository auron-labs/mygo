# Local changes

## Several documents in one window

`DocumentsOptions.Window` lets a window show several open documents one at a time, as tabs do (`Document.Select`, `OnSelect`, `OnClosed`); `DocumentsOptions.KeepWindow` keeps a window open after its last document closes. Folio depends on both.

- Paths: `document.go`, `document_flow.go`, `document_test.go`, `docs/documents.md`
- Check: `go test -run Document .`
- Upstream changes: port upstream's changes to documents onto these options; keep both options' behaviour.

## Rich text task boxes

`richtext.Theme.CheckboxOutline` draws tasks done as outlined boxes with a check in the `Checkbox` color.

- Paths: `ui/richtext/theme.go`, `ui/richtext/paint.go`
- Check: `go test ./ui/richtext`
- Upstream changes: take upstream's changes to the paint code and keep this option.

## Rendering fixes ahead of upstream

Metal keeps canvases' textures through an idle trim, a window paints again for a capture whose masks another window drew over, and `MYGO_ATLAS_LOG=1` logs the atlas generations of Direct3D frames.

- Paths: `internal/gpu/metal/`, `internal/gpu/d3d11/d3d11.go`, `internal/scene/scene.go`, `ui/window.go`, `ui/occluded_test.go`
- Check: `go test ./internal/gpu/... ./ui`
- Upstream changes: prefer upstream's implementation where it fixes the same behaviour; drop the local one then.

## Web view side buttons on macOS

A mouse's back and forward buttons over a web view reach the surface as `KeyBack` and `KeyForward`.

- Paths: `internal/darwin/webview.go`
- Check: manual, on macOS: press a mouse's side buttons over a web view.
- Upstream changes: prefer upstream's implementation where it does the same.

## Repository tooling

Dependabot opens one grouped weekly pull request (`.github/dependabot.yml`), and the agent skills live in `.agents/skills` and `.claude/skills`.

- Paths: `.github/dependabot.yml`, `.agents/skills/`, `.claude/skills/`
- Check: none
- Upstream changes: skip changes to these paths.
