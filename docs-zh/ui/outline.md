# 大纲

<!-- preview outline 320 -->

`ui.Outline` 以树形结构显示项目，类似 AppKit 的大纲视图和 Finder 的列表视图：先显示顶层项目，展开后在其下方缩进显示子项目，每项带有展开或收起的箭头。应用提供顶层项目和 `children` 函数，后者返回指定项目的子项目，没有子项目时返回 nil；与[列表](list.md)一样，大纲只构建可见行，因此大型树也能保持与小型树相同的性能。

```go
type fileTree struct {
	children map[string][]string // the paths in each directory
	files    ui.OutlineState[string]
	row      int
}

func (app *fileTree) view(c *ui.Context) {
	app.files.List.Selected = &app.row
	ui.Outline(c, &app.files, []string{"/"}, func(dir string) []string {
		return app.children[dir] // nil for a file
	}, func(path string) {
		ui.Text(c, filepath.Base(path))
	}).Grow(1)
	chosen := app.files.Item(app.row)
	// …
}
```

## 大纲状态

`OutlineState` 保存大纲状态：各行的 `ListState` 通过 `Selected`、`Selection` 控制选择，与列表的 `ListState` 相同；`Open` 则是包含所有已展开项目的 `Selection`。行以项目为键，因此上方项目展开或收起后，选择和行状态仍对应原来的项目。`Rows`、`Item` 和 `Depth` 用于读取当前显示的行。`Indent` 设置每深入一层在箭头前增加的缩进，为零时使用主题的 `Space(4)`，不能为负。`Prefix` 可自定义行首内容（[自己的行](#自己的行)）。

## 大型展开树

大纲在每一帧遍历所有已展开的项目来确定各行，开销与它们的数量成正比：像大型变更集的“显示全部更改”视图或“全部展开”那样展开数十万项时，每帧需要数毫秒。设置 `Generation` 可在帧之间保留各行：只有当 `Open`、顶层项目或 `Generation` 变化时，大纲才会重新遍历，因此每当 `children` 的返回结果变化时都要修改它。

```go
app.files.Generation = 1 // 创建大纲时设置一次

func (app *fileTree) refreshed(children map[string][]string) {
	app.children = children
	app.files.Generation++ // children 返回了不同的项目
}
```

## 打开和关闭

- 单击项目的箭头会打开或关闭它；按住 Option 单击（按住 Alt 单击）也会打开或关闭其中的所有项目。
- 大纲具有焦点时，Right 打开选中的项目或转到它的第一个子项目，Left 关闭它或转到它的父项目；在 macOS 上按住 Option、在其他平台上按住 Shift（如同 GTK），它们会打开或关闭其中的所有项目。
- 收起项目时，如果其子项目被选中，则改为选中被收起的项目。

## 自己的行

`OutlineState` 的 `Prefix` 函数可代替默认的缩进和箭头，构建每行的起始部分，例如 JSON 查看器的行，或带层级连接线的文件树。传入的 `OutlineRow` 提供项目深度（`Depth`）、是否有子项目（`Branch`）和是否已展开（`Open`）；`Arrow` 创建与默认箭头具有相同展开、收起行为的区域，由你设置尺寸并绘制。此时行不带内边距或间距，内容居中；键盘操作、选择和辅助功能仍由大纲处理。

```go
app.files.Prefix = func(r ui.OutlineRow) {
	ui.Box(c).Width(16 * float32(r.Depth)).Shrink(0)
	arrow := r.Arrow().Size(20, 28).Center()
	if r.Branch {
		arrow.Children(func() {
			icon := ui.Icon(c, chevron)
			if r.Open {
				icon.Rotate(90)
			}
		})
	}
}
```

## 在列中

`ui.OutlineTable` 在[表格](table.md)的列中提供同样的功能，第一列显示箭头，按项目的深度缩进：这些列就是表格的列，用户可以排序、调整大小和移动。

```go
ui.OutlineTable(c, &app.files, cols, roots, children, func(path string, col int) {
	switch col {
	case 0:
		ui.Text(c, filepath.Base(path)).SingleLine()
	case 1:
		ui.Text(c, app.size(path))
	}
})
```

## 辅助功能

辅助技术看到一棵树，或一个行可打开和关闭的表格，其中的行会说明自己的层级、能否打开、是否已打开，以及在所有行中的位置；它像箭头一样打开和关闭它们。
