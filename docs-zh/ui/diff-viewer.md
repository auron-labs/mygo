# 差异查看器

<!-- preview diff-viewer 340 -->

`editor.DiffView` 显示两段文本之间的差异，类似 Pierre 的差异视图和代码审查工具：可以并排显示，也可以合并在同一列中；行内改动的词会高亮，更改之间未变的行会折叠，新文本可以就地编辑。两侧各是一个[代码编辑器](code-editor.md)，支持相同的高亮、选区和命令，并且只对视口内的内容排版和绘制。

```go
import "github.com/egoist/mygo/ui/editor"

lang := editor.LanguageFor("main.go")
before, after := editor.NewBuffer(original), editor.NewBuffer(edited)
before.SetLanguage(lang)
after.SetLanguage(lang)
app.diff = &editor.DiffState{Old: before, New: after, Editable: true}

// In the view:
editor.DiffView(c, app.diff).Grow(1).FillWidth()
```

`DiffState` 是查看器的状态，跨帧保留：包括它比较的缓冲区、选项，以及它自己保留的状态，例如已展开的行和滚动位置。选项的零值就是默认值，也就是 Pierre 差异视图的默认值。

差异视图会跟随缓冲区：任一侧的编辑都会立刻反映出来，查看器只重新比较它附近的行。`New` 也可以是某个编辑器的缓冲区（例如某个文件的），这样差异视图会在输入时显示更改。

空文本按零行计算，与 `git diff` 一致：新增、删除或清空文件时，空文本一侧不显示行号，行号栏也不能选行或显示操作按钮。文本若以换行符结束，就不再计入其后的空行，与 git 一致：未启用 `Editable` 的差异视图会省略编辑器通常在该处显示的空行，折叠分隔符也不计入它；可编辑视图则保留该空行，供用户输入。

未启用 `Editable` 的差异视图比较行时会连同行尾换行符一起比较，git 也是如此，因此最后一行如果获得或失去换行符，就算作更改行；没有结尾换行符的文本，其最后一行末尾会以淡色显示“文件末尾没有换行”，也就是 git 的 `\ No newline at end of file`。`SetPatch` 的文本只包含补丁中的行，它会根据 `\` 行所指出的一侧打上标记。可编辑的差异视图比较行时不带换行符，也不做任何标记：添加结尾换行符会在其后新增一个空行，输入就落在那里。

## 布局

- `Layout` 可以左右并排显示旧文本和新文本，两边的行对齐（`DiffSplit`），一起滚动；也可以只显示新文本，把删除的行放在它们原来的位置上方（`DiffUnified`）。
- `SplitPercent` 指旧文本一侧占分栏视图宽度的百分比，0 表示各占一半。用户可以拖动分隔线调整，也可以用 Tab 聚焦分隔线后按方向键调整，操作与[分栏视图](split.md)相同。每侧至少保留行号栏和 12 列文本的宽度；视图太窄时，暂时均分宽度，但仍保存原比例，变宽后恢复。将该值存入应用设置，就能恢复用户上次使用的分栏比例：

  ```go
  app.diff.SplitPercent = app.settings.DiffSplit // 0 the first time: halves
  editor.DiffView(c, app.diff).Grow(1).FillWidth()
  app.settings.DiffSplit = app.diff.SplitPercent
  ```

  两侧会保持一起滚动，行也保持对齐：横向滚动时，每一侧最多滚到自己的行超出边缘的位置；启用 `Wrap` 时，如果一侧把某行折成较少的显示行，它下方就会留出另一侧对应行所占的空间。
- `Inline` 高亮更改行内部改动的内容：可以按词高亮，把只隔一个字符的词连起来，让一个短语看起来是一处改动（`DiffInlineWordAlt`，默认行为）；也可以逐个词高亮，从第一个词到最后一个词；还可以按字符高亮，或完全不高亮。
- `Indicators` 可以在行号栏边缘用色条标记更改行，并用斜纹表示删除行（`DiffBars`）；也可以在行前显示 + 和 −（`DiffClassic`），或只用背景色标记。`NoBackground` 隐藏更改行的底色，`NoLineNumbers` 隐藏行号。
- `ContextLines` 是每处更改周围显示的未变行数，默认是 3。其余的行会折叠成“N 行未修改”的分隔符，分隔符上的按钮每点击一次会多显示 `ExpansionLines` 行（默认 100），按住 Shift 则全部显示；`ExpandUnchanged`、`ExpandAll` 和 `CollapseAll` 可以显示或折叠全部行。
- `Wrap` 会对长行折行，每一侧的行仍与另一侧对应的行对齐；`FitContent` 让查看器的高度等于所有行的高度，适合在滚动视图中查看待审查的文件。
- `KeepScrollbars` 和 `NoScrollbars` 与代码编辑器相同：前者让指针离开后滚动条仍然显示，便于在填满窗口的差异视图中查看当前位置和可滚动范围；后者隐藏滚动条，并移除它们占用的空间。
- `Font`、`LineHeight`、`TabSize` 和 `Theme` 与代码编辑器相同。更改会用主题的 `Added`、`Deleted` 和 `Modified` 颜色着色，并在感知色彩空间中与背景混合，让它们无论在浅色还是深色模式下都清晰易读。

行比较采用 git 的 histogram diff 算法，无法拆分的部分再用 Myers 算法；随后按 git 的缩进启发式规则调整差异边界，让新增函数作为完整的一处更改显示，而不是显示成该函数末尾几行加上下一个函数开头几行。行内词语高亮会将更改行与最相似的行配对，而不是按行序配对。

## 编辑与审查

启用 `Editable` 时，新侧会像代码编辑器一样编辑：输入、多个插入符、撤销、查找以及所有命令，差异视图会跟随每次按键更新。Cmd+F8 和 Shift+Cmd+F8 转到下一处和上一处更改（其他平台上是 Alt+F5 和 Shift+Alt+F5），`GoToHunk` 则转到指定的更改。

- `HunkActions` 在指针下（或插入符所在）的更改上显示“还原”和“保留”：“还原”会把旧行放回 `New`，“保留”则让新行成为 `Old` 的内容，就像逐条审查智能体的更改那样。`Revert` 和 `Accept` 可以在应用里做同样的事；`Hunks` 列出各处的更改，`Stats` 统计新增和删除的行数。
- `LineSelection` 允许点击、拖动或 Shift+点击行号来选择行，`OnLineSelection` 会报告选区，`SelectedLines` 和 `SetSelectedLines` 用于读取和设置选区，例如审查中指向某些行的链接。
- `OnGutterButton` 在指针所在行的行号栏中显示 + 按钮，例如用于添加评论；单击按钮时会调用该回调，并传入对应行。
- `Annotations` 在行下方留出空间，供 `RenderAnnotation` 在那里构建内容，例如审查中的评论，每个高度由它自己决定；分栏视图的另一侧会把这块空间留空，以保持行对齐。

```go
app.diff.HunkActions = true
app.diff.OnGutterButton = func(r editor.DiffLineRange) {
	app.comments = append(app.comments, comment{side: r.Side, line: r.End})
}
app.diff.Annotations = app.diff.Annotations[:0]
for i, cm := range app.comments {
	app.diff.Annotations = append(app.diff.Annotations, editor.DiffAnnotation{Side: cm.side, Line: cm.line, Height: 72, Key: i})
}
app.diff.RenderAnnotation = func(c *ui.Context, a editor.DiffAnnotation) {
	app.commentView(c, a.Key.(int))
}
```

`ActiveSide` 是当前具有键盘焦点的一侧，或最后拥有焦点的一侧：分栏视图中默认是新侧，直到旧侧获得焦点。`Cursor` 和 `CursorPos` 返回该侧最新插入符的偏移和位置，和代码编辑器一样；`CurrentHunk` 返回插入符所在的更改，此时 `inside` 为 true，否则返回它前面的最后一处更改，在第一处之前则为 -1，就像“5 处中的第 2 处”的计数。`GoToHunk` 会移动该插入符，因此“下一处更改”可以从那里继续。`Run` 会在对应编辑器中像按键一样运行命令，`Find` 会按查询打开查找栏，就像菜单先接收按键的应用里菜单项所做的那样：

```go
if ui.Button(c, "Find").Clicked() {
	app.diff.Run(editor.Find)
}
i, inside := app.diff.CurrentHunk()
n := len(app.diff.Hunks())
label := fmt.Sprintf("%d changes", n) // before the first
if i >= 0 {
	label = fmt.Sprintf("%d of %d", i+1, n)
}
ui.Text(c, label)
if ui.Button(c, "Next change").Clicked() {
	app.diff.GoToHunk(i + 1)
}
if inside && ui.Button(c, "Revert this change").Clicked() {
	app.diff.Revert(i)
}
```

`ScrollY` 是差异视图的滚动距离，以 DIP 计；`ViewportHeight` 是视口中那部分的高度，取值来自上一帧的布局，无论滚动来自滚轮、按键、滚动条还是 `GoToHunk`；`SetScrollY` 可以滚动它。`LineTop(side, line)` 是任一侧某一行显示的位置，从差异视图顶部往下算；折叠的行不占空间；在统一视图中，删除的行位于其更改上方。传入某一侧的行数时，`LineTop` 返回整个差异视图的高度。应用可以据此在差异视图旁绘制小地图：画出其中的更改，以及一个表示当前视图位置的框，点击或拖动即可滚到那里：

```go
d := app.diff
var scale float64 // DIPs of the minimap per DIP of the diff, as last drawn
ui.Row(c).Fill().Children(func() {
	editor.DiffView(c, d).Grow(1).FillHeight()
	mini := ui.Box(c).Width(18).FillHeight()
	mini.Draw(func(p *ui.Painter, r ui.Rect) {
		scale = float64(r.H) / max(d.LineTop(editor.DiffNew, d.New.LineCount()), 1)
		at := func(y float64) float32 { return r.Y + float32(y*scale) }
		for _, h := range d.Hunks() {
			top := at(d.LineTop(editor.DiffNew, h.NewStart))
			bottom := at(d.LineTop(editor.DiffNew, max(h.NewEnd, h.NewStart+1)))
			p.Fill(ui.Rect{X: r.X, Y: top, W: r.W, H: max(bottom-top, 2)}, app.changeColor, 0)
		}
		view := ui.Rect{X: r.X, Y: at(d.ScrollY()), W: r.W, H: float32(d.ViewportHeight() * scale)}
		p.Stroke(view, c.Theme().Border, 0, 1)
	})
	mini.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind == ui.InputPointerDown || ev.Kind == ui.InputPointerMove && ev.Button == 0 {
			// The view centered where the minimap is pressed, or dragged.
			d.SetScrollY(float64(ev.Y)/max(scale, 1e-9) - d.ViewportHeight()/2)
			return true
		}
		return ev.Kind == ui.InputPointerUp
	})
})
```

## 样式

`Theme` 与[代码编辑器](code-editor.md#主题)的用法相同，也负责更改行的颜色。文本行周围的界面由应用构建，类似 Pierre 的差异视图：`RenderSeparator`、`RenderHunkActions` 和 `RenderGutterButton` 接收查看器已定位的元素，以及构建相应界面所需的数据和操作；也可以调用 `DefaultSeparator`、`DefaultHunkActions` 和 `DefaultGutterButton` 使用默认外观。`RenderFindBar` 和 `RenderGoToLine` 则分别构建两侧编辑器的查找栏和跳转行号栏，与代码编辑器相同。

```go
app.diff.RenderSeparator = func(c *ui.Context, sep ui.Element, s *editor.DiffSeparator) {
	sep.Padding(4).Children(func() {
		if s.Lines == 0 { // a patch's hunk
			ui.Text(c, s.Header).Font("monospace")
			return
		}
		if ui.Button(c, fmt.Sprintf("Show %d hidden lines", s.Lines)).Clicked() {
			s.ExpandAll()
		}
	})
}
app.diff.RenderHunkActions = func(c *ui.Context, bar ui.Element, a *editor.DiffHunkActions) {
	bar.Gap(4).Children(func() {
		if ui.Button(c, "Reject").Clicked() {
			a.Revert()
		}
		if ui.PrimaryButton(c, "Accept").Clicked() {
			a.Keep()
		}
	})
}
```

- `DiffSeparator` 横跨查看器，分栏时横跨两侧；查看器可滚动时，其宽度要减去滚动条轨道所占的空间（使用 `FitContent` 时则不减），高度为 32 DIP。它折叠 `Lines` 个未变行，`ExpandUp`、`ExpandDown` 和 `ExpandAll` 用于显示这些行，每次展开 `Step` 行；`First` 和 `Last` 标记第一处更改之前和最后一处更改之后的分隔符，它们只能朝一个方向展开。补丁的差异块则有一个零行的分隔符，并带有它的 `Header`。
- `DiffHunkActions` 位于指针下（或插入符所在）更改的右上角：`Index` 和 `Hunk` 说明是哪一个，`Revert` 和 `Keep` 执行操作。
- 行号栏按钮显示在指针所在行，单击时调用 `OnGutterButton`；其渲染函数负责构建外观和标签。

## 补丁

`editor.ParsePatch` 读取统一 diff 格式所更改的文件，和 `git diff`、`diff -u` 的输出格式一致，支持重命名、文件模式以及新增和删除的文件；`FilePatch.Apply` 把补丁应用到旧文件的文本上。`SetPatch` 只显示文件的补丁、不显示文件本身，就像收到拉取请求的差异时那样：显示其中各差异块的行，行号与文件中一致，每个差异块都在自己的标头下方。每个差异块单独高亮，从它第一行的开头开始，和 `git diff` 上色的方式一样，这样前一个差异块中未结束的注释或字符串就不会延续进来。语言自己的 `Highlighter`（[高亮器](code-editor.md#高亮器)）会把这些差异块的行当作一整段文本。

```go
files, err := editor.ParsePatch(output) // of git diff
if err != nil {
	return err
}
for _, f := range files {
	d := &editor.DiffState{}
	d.SetPatch(f)
	app.files = append(app.files, d)
}
```

如果应用也拿到了文件，例如 git 客户端从仓库中读出，`SetPatchFiles(f, old, new)` 就会显示完整文件以及补丁中的更改：即其中连续的 `-` 和 `+` 行，按 `git diff` 使用其选项（`--patience`、`--histogram`）后的对齐结果，而不是直接比较两个文本得到的结果。它们之间未变的行会折叠成可展开的分隔符，和其他差异视图一样。如果补丁是二进制文件，或无法从 `old` 得到 `new`，它会返回错误，并且不改变任何内容：

```go
old := app.fileAt("HEAD", f.OldName) // the app's own git read
new := app.fileAt("", f.NewName)      // the working tree's
if err := d.SetPatchFiles(f, old, new); err != nil {
	d.SetPatch(f) // the files moved on since the patch: show its hunks
}
```

## 辅助功能

辅助技术把每一侧看作一个文本框，名称分别是“旧文本”和“新文本”；旧侧只读，带有自己的文本和选区。分栏视图两侧之间的分隔线被看作名为“Divider”的分隔条，其值是旧侧的宽度，方向键可以移动它。每一侧里，视图中的每一行都是一个独立的元素，按显示顺序排列，名称是该行的文本，描述是它的行号（“第 12 行”）：新增的行是插入（insertion），删除的行是删除（deletion）（包括旧侧中改动的行，以及统一视图在新文本各行之间显示的已删除行，其行号是旧文本的，读作“旧文本第 12 行”），未改动的行是文本，这样屏幕阅读器就能把每一行读成它本来的样子。分隔符是一个组，名称来自它折叠的行数，描述说明是哪些行（“第 30 到 41 行”，旧文本和新文本的行号不同时分别给出），并带有向上、向下或全部展开这些行的按钮；“还原”和“保留”也是按钮，名称分别是“还原更改”和“保留更改”。这些名称以及查看器写出的内容（例如“4 行未修改”）都使用窗口的语言（[MyGo 自己的字符串](../i18n.md#mygo-自己的字符串)）。

```go
for _, a := range tt.AccessAll() {
	if a.Role == ui.RoleInsertion {
		fmt.Println("新增：", a.Description, a.Name) // 新增： Line 12 return nil
	}
}
```
