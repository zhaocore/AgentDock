package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhaocore/agentdock/internal/domain"
)

// packageReport 仅输出归档位置和计数，不输出正文。
type packageReport struct {
	Archive string `json:"archive"`
	Files   int    `json:"files"`
	Pages   int    `json:"pages"`
	Bytes   int64  `json:"bytes"`
}

// readLocalFile 限定在导出根目录内读取普通文件，拒绝符号链接和超大文件。
func readLocalFile(root *os.Root, name string) ([]byte, error) {
	if !domain.ValidConfluencePath(name) {
		return nil, fmt.Errorf("invalid library path %q", name)
	}
	parts := strings.Split(name, "/")
	for index := range parts {
		info, err := root.Lstat(strings.Join(parts[:index+1], "/"))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("library path contains symlink %q", name)
		}
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > domain.ConfluenceFileLimit {
		return nil, fmt.Errorf("library file must be regular and <= %d bytes: %s", domain.ConfluenceFileLimit, name)
	}
	content, err := io.ReadAll(io.LimitReader(file, domain.ConfluenceFileLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) != info.Size() {
		return nil, fmt.Errorf("library file size changed while reading %s", name)
	}
	return content, nil
}

// refreshedManifest 重算本地文件大小和校验和，保留页面来源、版本及修改时间。
func refreshedManifest(root *os.Root) (domain.ConfluenceArchiveManifest, packageReport, error) {
	var manifest domain.ConfluenceArchiveManifest
	var report packageReport
	raw, err := readLocalFile(root, domain.ConfluenceManifestName)
	if err != nil {
		return manifest, report, fmt.Errorf("read original manifest: %w", err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, report, fmt.Errorf("decode original manifest: %w", err)
	}
	if err := domain.ValidateConfluenceManifest(manifest); err != nil {
		return manifest, report, err
	}
	var expanded int64
	for spaceIndex, space := range manifest.Spaces {
		for pageIndex, page := range space.Pages {
			report.Pages++
			for fileIndex, file := range page.Files {
				content, err := readLocalFile(root, file.Path)
				if err != nil {
					return manifest, report, fmt.Errorf("read library file %s: %w", file.Path, err)
				}
				file.Size = int64(len(content))
				file.SHA256 = domain.ConfluenceFileHash(content)
				manifest.Spaces[spaceIndex].Pages[pageIndex].Files[fileIndex] = file
				expanded += file.Size
				if expanded > domain.ConfluenceExpandedLimit {
					return manifest, report, fmt.Errorf("library exceeds expanded limit %d bytes", domain.ConfluenceExpandedLimit)
				}
				report.Files++
			}
		}
	}
	manifest.ExportedAt = time.Now().UTC()
	return manifest, report, nil
}

// repackLibrary 仅打包原清单列出的文件，完整导入校验通过后发布不可覆盖的 ZIP。
func repackLibrary(directory, output string) (packageReport, error) {
	var report packageReport
	info, err := os.Lstat(directory)
	if err != nil {
		return report, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return report, fmt.Errorf("library root must be a directory without symlink: %s", directory)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return report, err
	}
	defer root.Close()
	manifest, report, err := refreshedManifest(root)
	if err != nil {
		return report, err
	}
	if _, err := os.Lstat(output); err == nil {
		return report, fmt.Errorf("output already exists; choose a new ZIP path: %s", output)
	} else if !os.IsNotExist(err) {
		return report, err
	}
	file, err := os.CreateTemp(filepath.Dir(output), ".confluence-repack-*.zip")
	if err != nil {
		return report, err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := domain.StreamConfluenceArchive(file, manifest, func(name string) ([]byte, error) {
		return readLocalFile(root, name)
	}); err != nil {
		return report, err
	}
	metadata, err := file.Stat()
	if err != nil {
		return report, err
	}
	if _, _, err := domain.OpenConfluenceArchive(file, metadata.Size()); err != nil {
		return report, fmt.Errorf("generated ZIP failed import validation: %w", err)
	}
	if err := file.Close(); err != nil {
		return report, err
	}
	if err := os.Link(file.Name(), output); err != nil {
		return report, fmt.Errorf("publish ZIP without overwriting: %w", err)
	}
	report.Archive = output
	report.Bytes = metadata.Size()
	return report, nil
}

// run 接收已修复的完整导出根目录和新 ZIP 路径。
func run(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: repack <export-root-with-confluence-library.json> <new-output.zip>")
	}
	report, err := repackLibrary(args[0], args[1])
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}

// main 失败返回非零，不发布未经验证的归档。
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
