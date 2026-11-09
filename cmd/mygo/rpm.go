package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"go.digitalxero.dev/rpm"
)

// rpmRelease is the package release number, 1 as in the PKGBUILD.
var rpmRelease = "1"

// rpmArch maps GOARCH to RPM architectures.
//
// See:
//   - https://github.com/rpm-software-management/rpm/blob/master/rpmrc.in
//   - https://github.com/goreleaser/nfpm/blob/main/rpm/rpm.go
var rpmArch = map[string]string{"amd64": "x86_64", "arm64": "aarch64", "386": "i386", "arm": "armv7hl", "riscv64": "riscv64"}

// rpmDepends returns the RPM requirements of the app: GTK and WebKitGTK by
// their shared library SONAMEs, which carry the "()(64bit)" marker on 64-bit
// architectures, followed by l.Depends as written, since "()(64bit)" applies
// only to shared library SONAMEs while l.Depends may name packages, version
// ranges or explicit capabilities.
func (l *Linux) rpmDepends(goarch string) []string {
	suffix := ""
	switch goarch {
	case "amd64", "arm64", "riscv64":
		suffix = "()(64bit)"
	}

	depends := []string{"libgtk-3.so.0" + suffix}
	if l.webView() {
		depends = append(depends, "libwebkit2gtk-4.1.so.0"+suffix)
	}
	return append(depends, l.Depends...)
}

// writeRpm packages the Linux app in stage with go.digitalxero.dev/rpm.
func writeRpm(c *Config, stage, name, goarch string, app []string) (string, error) {
	arch, ok := rpmArch[goarch]
	if !ok {
		return "", fmt.Errorf("no rpm architecture for: %s", goarch)
	}
	builder := rpm.NewPackage()
	files := rpmFiles{builder}

	opt := "opt/" + name
	files.dir(opt)

	for _, entry := range optEntries(app, name) {
		err := filepath.WalkDir(filepath.Join(stage, entry), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(stage, p)
			target := opt + "/" + filepath.ToSlash(rel)
			info, err := d.Info()
			if err != nil {
				return err
			}
			switch {
			case d.IsDir():
				files.dir(target)
			case info.Mode()&fs.ModeSymlink != 0:
				link, err := os.Readlink(p)
				if err != nil {
					return err
				}
				files.link(target, link)
			default:
				mode := uint(0o644)
				if info.Mode()&0o111 != 0 {
					mode = 0o755
				}
				files.fileFromPath(target, p, mode)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}

	for _, command := range c.Linux.Command {
		files.link("usr/bin/"+command, "../../"+opt+"/"+name)
	}
	if xml := mimePackage(c); xml != "" {
		// Types the app defines; the shared-mime-info trigger registers them.
		files.file("usr/share/mime/packages/"+name+".xml", []byte(xml), 0o644)
	}

	desktop := []byte(linuxDesktopEntry(c, "/"+opt+"/"+name, name))
	files.file("usr/share/applications/"+name+".desktop", desktop, 0o644)
	icons, err := linuxIcons(c)
	if err != nil {
		return "", err
	}
	for _, icon := range icons {
		dir := icon.dir()
		files.file(dir+"/"+name+".png", icon.png, 0o644)
	}
	description := c.Linux.Comment
	if description == "" {
		description = c.Name
	}

	builder.WithName(name).
		WithArch(arch).
		WithVersion(rpmVersion(c.Version)).
		WithPackager(c.Linux.Maintainer).
		WithSummary(description).
		WithDescription(description).
		WithRelease(rpmRelease).
		WithLicense(archLicense(c))

	depends := c.Linux.rpmDepends(goarch)
	requires := builder.Requires()
	for _, depend := range depends {
		requires.AddParsed(depend)
	}

	p, err := builder.Build()
	if err != nil {
		return "", err
	}

	outpath := filepath.Join(stage, fmt.Sprintf("%s-%s-%v.%s.rpm", name, rpmVersion(c.Version), rpmRelease, arch))
	out, err := os.Create(outpath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	_, err = p.WriteTo(out)
	if err != nil {
		return "", err
	}

	return outpath, nil
}

// rpmVersionRe matches what an rpm version cannot hold: it has letters,
// digits, the separators . _ + and the operators ~ ^, and no dash, which
// separates the components of a package's file name.
var rpmVersionRe = regexp.MustCompile(`[^A-Za-z0-9._+~^]+`)

// rpmVersion turns a semantic version into an rpm one: a pre-release sorts
// before its release with "~", as rpm's tilde makes it older, its dashes,
// which an rpm version cannot hold, become underscores as any other
// character, and build metadata follows with "+", a separator like any
// other, which sorts the version after the one without it, as a rebuild.
// rpm compares a numeric segment as newer than an alphabetic one, unlike
// semantic versioning, so a pre-release of numbers alone, as 1.2.0-1,
// sorts after one with letters, as 1.2.0-rc.1.
func rpmVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	v, build, _ := strings.Cut(v, "+")
	v, pre, _ := strings.Cut(v, "-")
	if pre != "" {
		v += "~" + pre
	}
	if build != "" {
		v += "+" + build
	}
	if v = rpmVersionRe.ReplaceAllString(v, "_"); v == "" {
		return "0"
	}
	return v
}

type rpmFiles struct {
	builder rpm.PackageBuilder
}

func (r *rpmFiles) dir(name string) {
	r.builder.File(r.toAbsolute(name)).Directory().WithMode(0o755)
}

func (r *rpmFiles) file(name string, data []byte, mode uint) {
	r.builder.WithFile(r.toAbsolute(name), data).WithMode(mode)
}

// fileFromPath streams a file from disk without reading it into memory.
func (r *rpmFiles) fileFromPath(name, path string, mode uint) {
	r.builder.WithFileFromPath(r.toAbsolute(name), path).WithMode(mode)
}

func (r *rpmFiles) link(name, linkTo string) {
	r.builder.File(r.toAbsolute(name)).WithSymlink(linkTo).WithMode(0o777)
}

// toAbsolute turns a path relative to the root into the absolute path the RPM builder takes.
func (r *rpmFiles) toAbsolute(name string) string {
	return path.Join("/", name)
}
