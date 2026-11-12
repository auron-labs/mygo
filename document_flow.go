package mygo

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The registry of documents, and what documents do with windows: asking
// to save before closing and quitting, and the File menu's commands.
// Dialogs are asked on goroutines, never in a loop nested on the main
// thread: a close or a quit that must ask is prevented, and done again
// once the document is saved or discarded, as VS Code does it.

var docRegistry struct {
	sync.Mutex
	kinds   []*docKind
	windows map[*Window]*docWindow
	// launched tells that the app delivered the files it started with;
	// pending are those it was asked to open before.
	launched bool
	pending  []string
	// role and open are what menus reach documents through, set with the
	// first kind, so that apps that make no documents link none of their
	// code.
	role func(MenuRole, *Window)
	open func(path string)
}

// registerDocKind adds k, and with the first kind sets documents up.
func registerDocKind(k *docKind) {
	docRegistry.Lock()
	docRegistry.kinds = append(docRegistry.kinds, k)
	docRegistry.role, docRegistry.open = documentRole, openDocumentPath
	first := len(docRegistry.kinds) == 1
	launched := docRegistry.launched
	docRegistry.Unlock()
	if launched {
		go k.restore()
		return
	}
	if !first {
		return
	}
	App.OnOpenFile(openDocumentFile)
	App.OnBeforeQuit(documentsBeforeQuit)
	App.OnQuit(func() {
		for _, k := range docKinds() {
			k.recovery.close()
		}
	})
	if runtime.GOOS == "darwin" {
		// Document apps stay open without windows, and make a document
		// when the Dock icon is clicked (applicationShouldOpenUntitledFile).
		App.OnWindowAllClosed(func() {})
		App.OnActivate(func(visible bool) {
			if k := defaultDocKind(); !visible && k != nil && !k.noUntitled && len(allDocuments()) == 0 {
				go k.newUntitled()
			}
		})
	}
	whenLaunched(startDocuments)
}

// startDocuments runs once the app delivered the files it started with:
// it restores the documents left unsaved, opens those it was asked to,
// or else makes an untitled document.
func startDocuments() {
	docRegistry.Lock()
	docRegistry.launched = true
	pending := docRegistry.pending
	docRegistry.pending = nil
	docRegistry.Unlock()
	go func() {
		for _, k := range docKinds() {
			k.restore()
		}
		for _, p := range pending {
			openFileOfKind(p)
		}
		if k := defaultDocKind(); k != nil && !k.noUntitled && len(allDocuments()) == 0 {
			k.newUntitled()
		}
	}()
}

// openDocumentFile opens a file the system asked the app to open, with
// the kind of documents of its extension.
func openDocumentFile(path string) {
	docRegistry.Lock()
	if !docRegistry.launched {
		docRegistry.pending = append(docRegistry.pending, path)
		docRegistry.Unlock()
		return
	}
	docRegistry.Unlock()
	go openFileOfKind(path)
}

// openFileOfKind opens path with the kind of documents of its extension,
// and tells why it could not.
func openFileOfKind(path string) {
	for _, k := range docKinds() {
		if k.handles(path) {
			if _, err := k.open(path); err != nil {
				showDocumentError(nil, menuMessages().DocumentFailed("opened", filepath.Base(path)), err)
			}
			return
		}
	}
}

// linkedDocuments returns what menus reach documents through, nil
// functions while the app has no kinds of documents.
func linkedDocuments() (role func(MenuRole, *Window), open func(path string)) {
	docRegistry.Lock()
	defer docRegistry.Unlock()
	if len(docRegistry.kinds) == 0 {
		return nil, nil
	}
	return docRegistry.role, docRegistry.open
}

func docKinds() []*docKind {
	docRegistry.Lock()
	defer docRegistry.Unlock()
	return slices.Clone(docRegistry.kinds)
}

// defaultDocKind is the kind RoleNew creates: the first.
func defaultDocKind() *docKind {
	docRegistry.Lock()
	defer docRegistry.Unlock()
	if len(docRegistry.kinds) == 0 {
		return nil
	}
	return docRegistry.kinds[0]
}

func allDocuments() []*docBase {
	var all []*docBase
	for _, k := range docKinds() {
		all = append(all, k.list()...)
	}
	return all
}

// openDocument returns the open document of the file at path.
func openDocument(path string) *docBase {
	info, err := os.Stat(path)
	for _, b := range allDocuments() {
		p := b.Path()
		if p == path {
			return b
		}
		if err == nil && p != "" {
			if other, err := os.Stat(p); err == nil && os.SameFile(info, other) {
				return b
			}
		}
	}
	return nil
}

// docWindow is a window that shows documents: one, or several it shows
// one at a time (DocumentsOptions.Window).
type docWindow struct {
	// docs are the documents in the order they opened in the window;
	// selected is the one it shows.
	docs     []*docBase
	selected *docBase
	// closing tells that the window waits for its edited documents to be
	// saved or answered for before it closes. Main thread only.
	closing bool
}

// documentFor returns the document w shows.
func documentFor(w *Window) *docBase {
	if w == nil {
		return nil
	}
	docRegistry.Lock()
	defer docRegistry.Unlock()
	if dw := docRegistry.windows[w]; dw != nil {
		return dw.selected
	}
	return nil
}

// windowDocuments returns the documents w shows, in the order they opened
// in it.
func windowDocuments(w *Window) []*docBase {
	docRegistry.Lock()
	defer docRegistry.Unlock()
	if dw := docRegistry.windows[w]; dw != nil {
		return slices.Clone(dw.docs)
	}
	return nil
}

// attach makes w the window of b, which it shows from now on. The first
// document of a window ties the window's closing to the documents. Main
// thread only.
func (b *docBase) attach(w *Window) {
	b.mu.Lock()
	b.window = w
	b.mu.Unlock()
	docRegistry.Lock()
	if docRegistry.windows == nil {
		docRegistry.windows = map[*Window]*docWindow{}
	}
	dw := docRegistry.windows[w]
	first := dw == nil
	if first {
		dw = &docWindow{}
		docRegistry.windows[w] = dw
	}
	dw.docs = append(dw.docs, b)
	dw.selected = b
	docRegistry.Unlock()
	fire(&b.onSelect)
	if !first {
		return
	}
	w.OnClose(windowClosing)
	w.OnClosed(func() { windowClosed(w) })
	w.OnFocus(func() {
		for _, d := range windowDocuments(w) {
			go d.checkDisk()
		}
		refreshDocumentMenus()
	})
	w.OnBlur(func() {
		for _, d := range windowDocuments(w) {
			d.mu.Lock()
			now := d.edited && d.autosavesLocked()
			d.mu.Unlock()
			if now {
				go d.persist()
			}
		}
		refreshDocumentMenus()
	})
	if p := w.Page(); p != nil {
		// The document names the window, not the page.
		p.OnPageTitleUpdated(func(e *TitleEvent) { e.PreventDefault() })
	}
}

// selectNow makes b the document its window shows. Main thread only.
func (b *docBase) selectNow() {
	b.mu.Lock()
	w := b.window
	b.mu.Unlock()
	docRegistry.Lock()
	dw := docRegistry.windows[w]
	changed := dw != nil && dw.selected != b && slices.Contains(dw.docs, b)
	if changed {
		dw.selected = b
	}
	docRegistry.Unlock()
	if !changed {
		return
	}
	showDocumentState(w, b.State())
	refreshDocumentMenus()
	fire(&b.onSelect)
}

// focus brings b's window to the front, showing b.
func (b *docBase) focus() {
	b.mu.Lock()
	w := b.window
	b.mu.Unlock()
	if w != nil {
		b.Select()
		w.Show()
		w.Focus()
	}
}

// settle saves b in place before it closes, or else asks what to do with
// its changes, showing it first, and reports whether it may close.
func (b *docBase) settle() bool {
	b.mu.Lock()
	autosaves, path := b.autosavesLocked(), b.path
	b.mu.Unlock()
	if autosaves {
		if _, err := b.write(path, writeAutosave); err == nil {
			return true
		}
	}
	b.Select()
	return b.askToSave()
}

// windowClosing decides whether a window of documents may close, on the
// main thread: it closes when its documents are unedited; edited ones are
// saved in place or asked about one after the other, which keeps the
// window open until they are answered. A canceled question keeps the
// window open with all its documents.
func windowClosing(e *CloseEvent) {
	w := e.Window
	docRegistry.Lock()
	dw := docRegistry.windows[w]
	docRegistry.Unlock()
	if dw == nil {
		return
	}
	docs := windowDocuments(w)
	if dw.closing || slices.ContainsFunc(docs, func(b *docBase) bool { return b.closing }) {
		e.PreventDefault()
		return
	}
	if e.DefaultPrevented() || App.quitting {
		return // the quit took care of the documents
	}
	edited := slices.DeleteFunc(docs, func(b *docBase) bool { return !b.IsEdited() })
	if len(edited) == 0 {
		return
	}
	e.PreventDefault()
	dw.closing = true
	go func() {
		ok := true
		for _, b := range edited {
			if ok = b.settle(); !ok {
				break
			}
		}
		onMain(func() {
			dw.closing = false
			if ok && w.native != nil {
				w.destroy()
			}
		})
	}()
}

// closeAlone closes b, whose window shows other documents too, once it
// is saved or its changes are answered for; the window stays open. Main
// thread only.
func (b *docBase) closeAlone() {
	b.mu.Lock()
	w := b.window
	b.mu.Unlock()
	docRegistry.Lock()
	dw := docRegistry.windows[w]
	docRegistry.Unlock()
	if b.closing || dw == nil || dw.closing {
		return
	}
	if !b.IsEdited() {
		b.leaveWindow(dw)
		return
	}
	b.closing = true
	go func() {
		ok := b.settle()
		onMain(func() {
			b.closing = false
			if ok {
				b.leaveWindow(dw)
			}
		})
	}()
}

// leaveWindow closes b, which leaves its window dw to its other
// documents; dw shows the one after b, or else the one before, when it
// showed b. Main thread only.
func (b *docBase) leaveWindow(dw *docWindow) {
	docRegistry.Lock()
	i := slices.Index(dw.docs, b)
	if i < 0 {
		docRegistry.Unlock()
		return
	}
	dw.docs = slices.Delete(dw.docs, i, i+1)
	var next *docBase
	if dw.selected == b {
		dw.selected = nil
		if len(dw.docs) > 0 {
			next = dw.docs[min(i, len(dw.docs)-1)]
		}
	}
	docRegistry.Unlock()
	b.windowClosed()
	if next != nil {
		next.selectNow()
	}
}

// windowClosed closes the documents of w, which closed. Main thread only.
func windowClosed(w *Window) {
	docRegistry.Lock()
	dw := docRegistry.windows[w]
	delete(docRegistry.windows, w)
	docRegistry.Unlock()
	if dw == nil {
		return
	}
	for _, b := range dw.docs {
		b.windowClosed()
	}
}

// windowClosed closes b, with its window or alone. Main thread only.
func (b *docBase) windowClosed() {
	b.mu.Lock()
	b.closed = true
	b.window = nil
	keep := b.edited && !b.discarded
	if b.timer != nil {
		b.timer.Stop()
	}
	b.mu.Unlock()
	k := b.kind
	k.mu.Lock()
	if i := slices.Index(k.docs, b); i >= 0 {
		k.docs = slices.Delete(k.docs, i, i+1)
	}
	k.mu.Unlock()
	b.watch()
	if keep {
		// Closed without asking (Window.Destroy, a parent closing, a quit
		// keeping documents): the changes stay in the recovery copy.
		docWork.Go(func() { b.backup() })
	} else {
		docWork.Go(func() { k.recovery.remove(b) })
	}
	refreshDocumentMenus()
	fire(&b.onClosed)
}

// discard drops the unsaved changes of b, which is closing.
func (b *docBase) discard() {
	b.mu.Lock()
	b.discarded = true
	b.mu.Unlock()
	b.kind.recovery.remove(b)
}

// parent is the window of b's dialogs: sheets on macOS.
func (b *docBase) parent() *Window {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.window
}

// buttons orders the buttons of a dialog asking to save, as each
// platform lays them out: on macOS the alert puts the first rightmost,
// the second left of it and the third apart, on the left. cancelAt is
// the Cancel button's.
func saveButtons(save, discard string) (buttons []string, saveAt, discardAt, cancelAt int) {
	cancel := menuMessages().Cancel()
	if runtime.GOOS == "darwin" {
		return []string{save, cancel, discard}, 0, 2, 1
	}
	return []string{save, discard, cancel}, 0, 1, 2
}

// askToSave asks whether to save b's changes before it closes, saves or
// discards them, and reports whether b may close.
func (b *docBase) askToSave() bool {
	s := b.State()
	tx := menuMessages()
	var opts MessageOptions
	var buttons []string
	var saveAt, discardAt, cancelAt int
	if s.Path == "" && b.kind.autosave == AutosaveInPlace {
		buttons, saveAt, discardAt, cancelAt = saveButtons(tx.SaveEllipsis(), tx.DeleteDocument())
		opts = MessageOptions{
			Message: tx.KeepNewDocument(s.Name),
			Detail:  tx.KeepNewDocumentDetail(),
		}
	} else {
		save := tx.Save()
		if s.Path == "" {
			save = tx.SaveEllipsis()
		}
		buttons, saveAt, discardAt, cancelAt = saveButtons(save, tx.DontSave())
		opts = MessageOptions{
			Message: tx.SaveChanges(s.Name),
			Detail:  tx.ChangesLost(),
		}
	}
	opts.Parent, opts.Type, opts.Buttons = b.parent(), MessageWarning, buttons
	opts.CancelButton = cancelAt
	r, err := Dialog.Message(opts)
	if err != nil {
		return false
	}
	switch r.Button {
	case saveAt:
		return b.saveOrShow() == nil
	case discardAt:
		b.discard()
		return true
	}
	return false
}

// saveOrShow saves b as File > Save does, and shows why it failed.
func (b *docBase) saveOrShow() error {
	err := b.saveInteractive(false)
	if err != nil && !errors.Is(err, ErrCanceled) {
		showDocumentError(b.parent(), menuMessages().DocumentFailed("saved", b.Name()), err)
	}
	return err
}

// saveInteractive saves b, asking for a file when it has none or as is
// true, and what to do when its file can't be written or another app
// changed it.
func (b *docBase) saveInteractive(as bool) error {
	b.mu.Lock()
	path, readOnly := b.path, b.readOnly
	b.mu.Unlock()
	if as || path == "" {
		target, err := b.askPath("", "", "")
		if err != nil {
			return err
		}
		_, err = b.write(target, writeSaveAs)
		return err
	}
	if !readOnly {
		_, err := b.write(path, writeSave)
		switch {
		case errors.Is(err, ErrDocumentChanged):
			return b.resolveConflict(path)
		case errors.Is(err, fs.ErrPermission):
			readOnly = true
		default:
			return err
		}
	}
	tx := menuMessages()
	buttons := []string{tx.SaveAs(), tx.Cancel()}
	r, err := Dialog.Message(MessageOptions{
		Parent: b.parent(), Type: MessageWarning, Buttons: buttons, CancelButton: 1,
		Message: tx.ReadOnly(b.Name()),
		Detail:  tx.ReadOnlyDetail(),
	})
	if err != nil || r.Button != 0 {
		return ErrCanceled
	}
	return b.saveInteractive(true)
}

// resolveConflict asks what to do when another app changed b's file.
func (b *docBase) resolveConflict(path string) error {
	tx := menuMessages()
	buttons, saveAt, revertAt, cancelAt := saveButtons(tx.SaveAnyway(), tx.RevertDocument())
	r, err := Dialog.Message(MessageOptions{
		Parent: b.parent(), Type: MessageWarning, Buttons: buttons, CancelButton: cancelAt,
		Message: tx.ChangedOnDisk(filepath.Base(path)),
		Detail:  tx.ChangedOnDiskDetail(),
	})
	if err != nil {
		return err
	}
	switch r.Button {
	case saveAt:
		_, err := b.write(path, writeOverwrite)
		return err
	case revertAt:
		return b.reload(path)
	}
	return ErrCanceled
}

// askPath asks where to save b, or to rename it (title and button
// "Rename", nameLabel "Rename To:").
func (b *docBase) askPath(title, button, nameLabel string) (string, error) {
	b.mu.Lock()
	path, name := b.path, b.nameLocked()
	b.mu.Unlock()
	ext := b.kind.types[0].Extensions[0]
	def := path
	if def == "" {
		dir, _ := App.Path(PathDocuments)
		def = filepath.Join(dir, name+"."+ext)
	}
	opts := SaveDialogOptions{Parent: b.parent(), Title: title, ButtonLabel: button, DefaultPath: def, Filters: b.kind.filters()}
	opts.NameFieldLabel = nameLabel
	target, err := Dialog.Save(opts)
	if err != nil {
		return "", err
	}
	if target == "" {
		return "", ErrCanceled
	}
	if filepath.Ext(target) == "" {
		target += "." + ext // GTK's dialog adds none
	}
	return target, nil
}

// duplicate makes an untitled copy of b, as File > Duplicate does.
func (b *docBase) duplicate() *docBase {
	snap := b.ops.capture()
	name := b.Name()
	if b.Path() != "" {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	d, _ := b.kind.create(snap.value, name+" copy")
	d.id = randomText()
	d.ops.restore(snap.value)
	d.dirtySince = time.Now()
	b.kind.show(d)
	return d
}

// rename renames b's file to name, in its directory; "" asks for it.
func (b *docBase) rename(name string) error {
	path := b.Path()
	if path == "" {
		return b.saveInteractive(true)
	}
	var target string
	if name == "" {
		var err error
		tx := menuMessages()
		if target, err = b.askPath(tx.Rename(), tx.Rename(), tx.RenameTo()); err != nil {
			return err
		}
	} else {
		if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
			return fmt.Errorf("mygo: invalid file name %q", name)
		}
		target = filepath.Join(filepath.Dir(path), name)
		if _, err := os.Lstat(target); err == nil {
			return fmt.Errorf("mygo: %s already exists", target)
		}
	}
	return b.relocate(target)
}

// moveTo moves b's file into dir; "" asks for it.
func (b *docBase) moveTo(dir string) error {
	path := b.Path()
	if dir == "" {
		tx := menuMessages()
		dirs, err := Dialog.Open(OpenDialogOptions{Parent: b.parent(), Title: tx.MoveTo(), ButtonLabel: tx.Move(), Directory: true, CreateDirectories: true})
		if err != nil {
			return err
		}
		if len(dirs) == 0 {
			return ErrCanceled
		}
		dir = dirs[0]
	}
	if path == "" {
		_, err := b.write(filepath.Join(dir, b.Name()+"."+b.kind.types[0].Extensions[0]), writeSaveAs)
		return err
	}
	target := filepath.Join(dir, filepath.Base(path))
	if target == path {
		return nil
	}
	if _, err := os.Lstat(target); err == nil {
		tx := menuMessages()
		r, err := Dialog.Message(MessageOptions{
			Parent: b.parent(), Type: MessageWarning, Buttons: []string{tx.ReplaceFile(), tx.Cancel()}, CancelButton: 1,
			Message: tx.AlreadyExists(filepath.Base(target)),
			Detail:  tx.AlreadyExistsDetail(),
		})
		if err != nil || r.Button != 0 {
			return ErrCanceled
		}
	}
	return b.relocate(target)
}

// relocate moves b's file to target, which the document then is.
func (b *docBase) relocate(target string) error {
	b.io.Lock()
	path := b.Path()
	err := os.Rename(path, target)
	if errors.Is(err, syscall.EXDEV) {
		err = copyFile(path, target)
		if err == nil {
			err = os.Remove(path)
		}
	}
	if err != nil {
		b.io.Unlock()
		return err
	}
	stamp, _, _ := statStamp(target)
	b.mu.Lock()
	b.path, b.stamp = target, stamp
	b.readOnly = !fileWritable(target)
	b.mu.Unlock()
	b.io.Unlock()
	addRecent(target)
	b.watch()
	b.notify()
	return nil
}

func copyFile(from, to string) error {
	src, err := openShared(from)
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = writeFileSafely(to, func(w io.Writer) error {
		_, err := io.Copy(w, src)
		return err
	})
	return err
}

// revert asks whether to discard b's changes, then reads its file again.
func (b *docBase) revert() error {
	s := b.State()
	if !s.Edited || s.Path == "" {
		return nil
	}
	tx := menuMessages()
	r, err := Dialog.Message(MessageOptions{
		Parent: b.parent(), Type: MessageWarning, Buttons: []string{tx.RevertDocument(), tx.Cancel()}, CancelButton: 1,
		Message: tx.RevertQuestion(s.Name),
		Detail:  tx.RevertDetail(),
	})
	if err != nil || r.Button != 0 {
		return ErrCanceled
	}
	return b.reload(s.Path)
}

// showDocumentError tells the user what went wrong.
func showDocumentError(parent *Window, message string, err error) {
	detail := err.Error()
	var pe *fs.PathError
	if errors.As(err, &pe) {
		detail = pe.Err.Error()
	}
	if _, derr := Dialog.Message(MessageOptions{Parent: parent, Type: MessageError, Message: message, Detail: detail}); derr != nil {
		log.Printf("mygo: documents: %s %v", message, err)
	}
}

// The quit: documents are saved in place, kept in their recovery copies
// for the next launch, or asked about (DocumentsOptions.AskOnQuit). The
// quit waits for that, then starts again. Main thread only.
var docQuit struct{ running, resolved bool }

// quitAgain quits once the documents are taken care of; tests replace it.
var quitAgain = func() { App.Quit() }

func documentsBeforeQuit(e *QuitEvent) {
	if docQuit.resolved {
		docQuit.resolved = false
		return
	}
	if e.DefaultPrevented() && !e.SessionEnding {
		return
	}
	if docQuit.running && !e.SessionEnding {
		e.PreventDefault()
		return
	}
	var edited []*docBase
	for _, b := range allDocuments() {
		if b.IsEdited() {
			edited = append(edited, b)
		}
	}
	if len(edited) == 0 {
		return
	}
	if e.SessionEnding {
		// The OS ends the session: no question, and nothing to wait for.
		// Each document is saved in place or kept, right away.
		for _, b := range edited {
			b.mu.Lock()
			autosaves, path := b.autosavesLocked(), b.path
			b.mu.Unlock()
			if autosaves {
				if _, err := b.write(path, writeAutosave); err == nil {
					continue
				}
			}
			b.backup()
		}
		return
	}
	e.PreventDefault()
	docQuit.running = true
	go func() {
		ok := resolveQuit(edited)
		onMain(func() {
			docQuit.running = false
			if ok {
				docQuit.resolved = true
				quitAgain()
			}
		})
	}()
}

// resolveQuit takes care of the edited documents before quitting, and
// reports whether the app may quit.
func resolveQuit(docs []*docBase) bool {
	var ask []*docBase
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, b := range docs {
		b.mu.Lock()
		autosaves, path := b.autosavesLocked(), b.path
		b.mu.Unlock()
		if b.kind.askOnQuit && !autosaves {
			ask = append(ask, b)
			continue
		}
		wg.Go(func() {
			if autosaves {
				if _, err := b.write(path, writeAutosave); err == nil {
					return
				}
			}
			if b.kind.askOnQuit {
				mu.Lock()
				ask = append(ask, b)
				mu.Unlock()
				return
			}
			b.backup()
		})
	}
	wg.Wait()
	switch len(ask) {
	case 0:
		return true
	case 1:
		ask[0].focus()
		return ask[0].askToSave()
	}
	if runtime.GOOS == "darwin" {
		tx := menuMessages()
		buttons := []string{tx.ReviewChanges(), tx.Cancel(), tx.DiscardChanges()}
		r, err := Dialog.Message(MessageOptions{
			Type: MessageWarning, Buttons: buttons, CancelButton: 1,
			Message: tx.ReviewBeforeQuitting(len(ask), App.Name()),
			Detail:  tx.ReviewBeforeQuittingDetail(),
		})
		if err != nil {
			return false
		}
		switch r.Button {
		case 0:
			for _, b := range ask {
				b.focus()
				if !b.askToSave() {
					return false
				}
			}
			return true
		case 2:
			for _, b := range ask {
				b.discard()
			}
			return true
		}
		return false
	}
	names := make([]string, len(ask))
	for i, b := range ask {
		names[i] = b.Name()
	}
	tx := menuMessages()
	r, err := Dialog.Message(MessageOptions{
		Type: MessageWarning, Buttons: []string{tx.SaveAll(), tx.DontSave(), tx.Cancel()}, CancelButton: 2,
		Message: tx.SaveManyChanges(len(ask)),
		Detail:  strings.Join(names, "\n") + "\n\n" + tx.ChangesLost(),
	})
	if err != nil {
		return false
	}
	switch r.Button {
	case 0:
		for _, b := range ask {
			b.focus()
			if b.saveOrShow() != nil {
				return false
			}
		}
		return true
	case 1:
		for _, b := range ask {
			b.discard()
		}
		return true
	}
	return false
}
