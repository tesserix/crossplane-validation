package lint

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// toolInstaller describes how to download and install a tool.
type toolInstaller struct {
	Name    string
	Binary  string // binary name after install
	URLFunc func(goos, goarch string) string
	Extract func(archivePath, destDir, binaryName string) error
	PostCmd []string // optional post-install command
}

var installers = []toolInstaller{
	{
		Name:   "kubeconform",
		Binary: "kubeconform",
		URLFunc: func(goos, goarch string) string {
			os := goos
			if os == "darwin" {
				os = "darwin"
			}
			arch := goarch
			if arch == "amd64" {
				arch = "amd64"
			}
			ext := "tar.gz"
			if goos == "windows" {
				ext = "zip"
			}
			return fmt.Sprintf("https://github.com/yannh/kubeconform/releases/latest/download/kubeconform-%s-%s.%s", os, arch, ext)
		},
		Extract: extractTarGz,
	},
	{
		Name:   "pluto",
		Binary: "pluto",
		URLFunc: func(goos, goarch string) string {
			// Pluto includes version in filename — resolve latest via redirect
			version := resolveLatestTag("FairwindsOps/pluto")
			if version == "" {
				return ""
			}
			ext := "tar.gz"
			if goos == "windows" {
				ext = "zip"
			}
			return fmt.Sprintf("https://github.com/FairwindsOps/pluto/releases/download/%s/pluto_%s_%s_%s.%s",
				version, strings.TrimPrefix(version, "v"), goos, goarch, ext)
		},
		Extract: extractTarGz,
	},
	{
		Name:   "kube-linter",
		Binary: "kube-linter",
		URLFunc: func(goos, goarch string) string {
			// kube-linter uses versioned filenames too
			version := resolveLatestTag("stackrox/kube-linter")
			if version == "" {
				return ""
			}
			ext := "tar.gz"
			if goos == "windows" {
				ext = "zip"
			}
			return fmt.Sprintf("https://github.com/stackrox/kube-linter/releases/download/%s/kube-linter-%s_%s.%s",
				version, goos, goarch, ext)
		},
		Extract: extractTarGz,
	},
	{
		Name:   "yamllint",
		Binary: "yamllint",
		URLFunc: func(_, _ string) string {
			return "" // pip install, handled separately
		},
	},
	{
		Name:   "crossplane",
		Binary: "crossplane",
		URLFunc: func(goos, goarch string) string {
			return fmt.Sprintf("https://releases.crossplane.io/stable/current/bin/%s_%s/crank", goos, goarch)
		},
		Extract: extractRawBinary,
	},
}

// InstallDir returns the directory where tools are installed.
// Uses ~/.crossplane-validate/tools/ to avoid requiring sudo.
func InstallDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "crossplane-validate-tools")
	}
	return filepath.Join(home, ".crossplane-validate", "tools")
}

// EnsureTools installs any missing tools. Returns list of newly installed tools.
func EnsureTools(toolNames []string) ([]string, error) {
	installDir := InstallDir()
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return nil, fmt.Errorf("creating tool directory %s: %w", installDir, err)
	}

	var installed []string
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	for _, inst := range installers {
		// Skip if not requested
		if len(toolNames) > 0 && !contains(toolNames, inst.Name) && !contains(toolNames, inst.Binary) {
			continue
		}

		// Skip if already available
		if binaryExists(inst.Binary) {
			continue
		}

		// Check if it's in our local tool dir
		localPath := filepath.Join(installDir, inst.Binary)
		if _, err := os.Stat(localPath); err == nil {
			continue
		}

		// Handle yamllint (Python) separately
		if inst.Name == "yamllint" {
			if installYamllint() {
				installed = append(installed, "yamllint")
			}
			continue
		}

		url := inst.URLFunc(goos, goarch)
		if url == "" {
			continue
		}

		fmt.Fprintf(os.Stderr, "  Installing %s...\n", inst.Name)

		if err := downloadAndInstall(url, installDir, inst); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not install %s: %v\n", inst.Name, err)
			continue
		}

		installed = append(installed, inst.Name)
		fmt.Fprintf(os.Stderr, "  Installed %s to %s\n", inst.Name, installDir)
	}

	return installed, nil
}

// AddToolDirToPath returns the install dir so callers can prepend it to PATH.
func AddToolDirToPath() string {
	return InstallDir()
}

func installYamllint() bool {
	// Try pip3 first, then pip, then pipx
	for _, pip := range []string{"pip3", "pip", "pipx"} {
		if binaryExists(pip) {
			cmd := exec.Command(pip, "install", "--user", "--quiet", "yamllint")
			cmd.Stderr = os.Stderr
			if cmd.Run() == nil {
				return true
			}
		}
	}
	return false
}

func downloadAndInstall(url, destDir string, inst toolInstaller) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download returned %d for %s", resp.StatusCode, url)
	}

	tmpFile, err := os.CreateTemp("", "cv-tool-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	if inst.Extract != nil {
		return inst.Extract(tmpFile.Name(), destDir, inst.Binary)
	}
	return nil
}

func extractTarGz(archivePath, destDir, binaryName string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		// Try as zip
		return extractZip(archivePath, destDir, binaryName)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		base := filepath.Base(hdr.Name)
		if base == binaryName || strings.TrimSuffix(base, ".exe") == binaryName {
			destPath := filepath.Join(destDir, binaryName)
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
			return nil
		}
	}

	return fmt.Errorf("binary %s not found in archive", binaryName)
}

func extractZip(archivePath, destDir, binaryName string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		base := filepath.Base(f.Name)
		if base == binaryName || strings.TrimSuffix(base, ".exe") == binaryName {
			src, err := f.Open()
			if err != nil {
				return err
			}
			defer src.Close()

			destPath := filepath.Join(destDir, binaryName)
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, src); err != nil {
				out.Close()
				return err
			}
			out.Close()
			return nil
		}
	}

	return fmt.Errorf("binary %s not found in zip", binaryName)
}

// resolveLatestTag gets the latest release tag for a GitHub repo via redirect.
func resolveLatestTag(repo string) string {
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(fmt.Sprintf("https://github.com/%s/releases/latest", repo))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc == "" {
		return ""
	}
	parts := strings.Split(loc, "/")
	return parts[len(parts)-1]
}

func extractRawBinary(archivePath, destDir, binaryName string) error {
	destPath := filepath.Join(destDir, binaryName)
	src, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer src.Close()

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, src)
	return err
}
