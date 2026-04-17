package dmcunrar

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Test helpers

func testdataPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

func openTestArchive(t *testing.T, name string) *Archive {
	t.Helper()
	archive, err := OpenArchiveFromPath(testdataPath(t, name))
	if err != nil {
		t.Fatalf("failed to open %s: %v", name, err)
	}
	t.Cleanup(func() { archive.Free() })
	return archive
}

func extractToBytes(t *testing.T, archive *Archive, index int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	ef := NewExtractedFile(&buf)
	defer ef.Free()
	if err := archive.ExtractFile(ef, index); err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	return buf.Bytes()
}

// Opening Archives Tests

func TestOpenArchiveFromPath(t *testing.T) {
	tests := []struct {
		name    string
		archive string
		wantErr bool
	}{
		{"RAR5 simple", "simple.rar", false},
		{"RAR5 explicit", "simple_rar5.rar", false},
		{"multiple files", "multiple_files.rar", false},
		{"with directories", "with_dirs.rar", false},
		{"compressed", "compressed.rar", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive, err := OpenArchiveFromPath(testdataPath(t, tt.archive))
			if (err != nil) != tt.wantErr {
				t.Errorf("OpenArchiveFromPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if archive != nil {
				archive.Free()
			}
		})
	}
}

func TestOpenArchiveFromPath_NotFound(t *testing.T) {
	_, err := OpenArchiveFromPath(testdataPath(t, "nonexistent.rar"))
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestOpenArchiveFromPath_NotRar(t *testing.T) {
	_, err := OpenArchiveFromPath(testdataPath(t, "not_a_rar.txt"))
	if err == nil {
		t.Error("expected error for non-RAR file, got nil")
	}
}

func TestOpenArchive(t *testing.T) {
	path := testdataPath(t, "simple.rar")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}

	reader := bytes.NewReader(data)
	archive, err := OpenArchive(reader, int64(len(data)))
	if err != nil {
		t.Fatalf("OpenArchive() error = %v", err)
	}
	defer archive.Free()

	// Verify it works by checking file count
	if count := archive.GetFileCount(); count != 1 {
		t.Errorf("GetFileCount() = %d, want 1", count)
	}
}

// Metadata Tests

func TestGetFileCount(t *testing.T) {
	tests := []struct {
		archive string
		want    int64
	}{
		{"simple.rar", 1},
		{"simple_rar5.rar", 1},
		{"multiple_files.rar", 3},
		{"with_dirs.rar", 1}, // directory entry may or may not be included
		{"compressed.rar", 1},
	}

	for _, tt := range tests {
		t.Run(tt.archive, func(t *testing.T) {
			archive := openTestArchive(t, tt.archive)
			got := archive.GetFileCount()
			if got != tt.want {
				t.Errorf("GetFileCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetFilename(t *testing.T) {
	tests := []struct {
		archive  string
		index    int64
		wantName string
	}{
		{"simple.rar", 0, "hello.txt"},
		{"multiple_files.rar", 0, "a.txt"},
		{"multiple_files.rar", 1, "b.txt"},
		{"multiple_files.rar", 2, "c.txt"},
		{"with_dirs.rar", 0, "dir1/file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.archive+"/"+tt.wantName, func(t *testing.T) {
			archive := openTestArchive(t, tt.archive)
			name, err := archive.GetFilename(tt.index)
			if err != nil {
				t.Fatalf("GetFilename() error = %v", err)
			}
			if name != tt.wantName {
				t.Errorf("GetFilename() = %q, want %q", name, tt.wantName)
			}
		})
	}
}

func TestGetFileStat(t *testing.T) {
	tests := []struct {
		archive  string
		index    int64
		wantSize int64
	}{
		{"simple.rar", 0, 13},        // "Hello, World!" = 13 bytes
		{"multiple_files.rar", 0, 9}, // "Content A" = 9 bytes
		{"compressed.rar", 0, 13},
		{"with_dirs.rar", 0, 16}, // "Dir file content" = 16 bytes
	}

	for _, tt := range tests {
		t.Run(tt.archive, func(t *testing.T) {
			archive := openTestArchive(t, tt.archive)
			stat := archive.GetFileStat(tt.index)
			if stat == nil {
				t.Fatal("GetFileStat() returned nil")
			}
			size := stat.GetUncompressedSize()
			if size != tt.wantSize {
				t.Errorf("GetUncompressedSize() = %d, want %d", size, tt.wantSize)
			}
		})
	}
}

func TestFileIsDirectory(t *testing.T) {
	// Test that regular files are not directories
	archive := openTestArchive(t, "simple.rar")
	if archive.FileIsDirectory(0) {
		t.Error("FileIsDirectory() = true for regular file, want false")
	}

	// Test file in subdirectory path (still a file, not a directory)
	archive2 := openTestArchive(t, "with_dirs.rar")
	if archive2.FileIsDirectory(0) {
		t.Error("FileIsDirectory() = true for file in subdir, want false")
	}
}

func TestFileIsSupported(t *testing.T) {
	tests := []struct {
		archive string
		index   int64
	}{
		{"simple.rar", 0},
		{"simple_rar5.rar", 0},
		{"compressed.rar", 0},
		{"multiple_files.rar", 0},
	}

	for _, tt := range tests {
		t.Run(tt.archive, func(t *testing.T) {
			archive := openTestArchive(t, tt.archive)
			err := archive.FileIsSupported(tt.index)
			if err != nil {
				t.Errorf("FileIsSupported() = %v, want nil", err)
			}
		})
	}
}

func TestFileIsSupportedEncryptedFile(t *testing.T) {
	archive := openGeneratedEncryptedArchive(t, "-psecret")

	err := archive.FileIsSupported(0)
	if err == nil {
		t.Fatal("FileIsSupported() error = nil, want encrypted error")
	}
	if !errors.Is(err, ErrEncrypted) {
		t.Fatalf("FileIsSupported() error = %v, want ErrEncrypted", err)
	}

	var unrarErr *Error
	if !errors.As(err, &unrarErr) {
		t.Fatalf("FileIsSupported() error = %T, want *Error", err)
	}
	if unrarErr.Operation != "dmc_unrar_file_is_supported" {
		t.Fatalf("Operation = %q, want dmc_unrar_file_is_supported", unrarErr.Operation)
	}
	if unrarErr.Code != ErrorCodeFileUnsupportedEncrypted {
		t.Fatalf("Code = %d, want %d", unrarErr.Code, ErrorCodeFileUnsupportedEncrypted)
	}
	if !unrarErr.IsEncrypted() {
		t.Fatal("IsEncrypted() = false, want true")
	}
}

func openGeneratedEncryptedArchive(t *testing.T, passwordFlag string) *Archive {
	t.Helper()
	archivePath := generateEncryptedArchive(t, passwordFlag)
	archive, err := OpenArchiveFromPath(archivePath)
	if err != nil {
		t.Fatalf("failed to open generated archive: %v", err)
	}
	t.Cleanup(func() { archive.Free() })
	return archive
}

func generateEncryptedArchive(t *testing.T, passwordFlag string) string {
	t.Helper()
	if _, err := exec.LookPath("rar"); err != nil {
		t.Skip("rar command not available")
	}

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(sourcePath, []byte("secret\n"), 0o644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	archivePath := filepath.Join(tmpDir, "encrypted.rar")
	cmd := exec.Command("rar", "a", "-idq", passwordFlag, "encrypted.rar", "secret.txt")
	cmd.Dir = tmpDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rar failed: %v\n%s", err, output)
	}

	return archivePath
}

func TestArchiveEncryptedErrorMatchesSentinel(t *testing.T) {
	err := &Error{
		Operation: "dmc_unrar_archive_open",
		Code:      ErrorCodeArchiveUnsupportedEncrypted,
		Message:   "Unsupported archive feature: encryption",
	}

	if !errors.Is(err, ErrEncrypted) {
		t.Fatalf("errors.Is(%v, ErrEncrypted) = false, want true", err)
	}
	if !err.IsEncrypted() {
		t.Fatal("IsEncrypted() = false, want true")
	}
}

// Extraction Tests

func TestExtractFile(t *testing.T) {
	archive := openTestArchive(t, "simple.rar")
	content := extractToBytes(t, archive, 0)

	want := "Hello, World!"
	if string(content) != want {
		t.Errorf("extracted content = %q, want %q", string(content), want)
	}
}

func TestExtractFile_Compressed(t *testing.T) {
	archive := openTestArchive(t, "compressed.rar")
	content := extractToBytes(t, archive, 0)

	want := "Hello, World!"
	if string(content) != want {
		t.Errorf("extracted content = %q, want %q", string(content), want)
	}
}

func TestExtractFile_AllFiles(t *testing.T) {
	archive := openTestArchive(t, "multiple_files.rar")

	expected := []struct {
		index   int64
		content string
	}{
		{0, "Content A"},
		{1, "Content B"},
		{2, "Content C"},
	}

	for _, exp := range expected {
		content := extractToBytes(t, archive, exp.index)
		if string(content) != exp.content {
			t.Errorf("file %d: got %q, want %q", exp.index, string(content), exp.content)
		}
	}
}

func TestExtractFile_SubdirFile(t *testing.T) {
	archive := openTestArchive(t, "with_dirs.rar")
	content := extractToBytes(t, archive, 0)

	want := "Dir file content"
	if string(content) != want {
		t.Errorf("extracted content = %q, want %q", string(content), want)
	}
}

// Resource Management Tests

func TestArchiveFree(t *testing.T) {
	archive, err := OpenArchiveFromPath(testdataPath(t, "simple.rar"))
	if err != nil {
		t.Fatalf("failed to open archive: %v", err)
	}

	// Free should not panic
	archive.Free()

	// Double free should not panic
	archive.Free()
}

func TestFileReaderSeek(t *testing.T) {
	data := []byte("test data for seeking")
	reader := bytes.NewReader(data)
	fr := NewFileReader(reader, int64(len(data)))
	defer fr.Free()

	tests := []struct {
		name   string
		offset int64
		whence int
		want   int64
	}{
		{"SeekStart", 5, io.SeekStart, 5},
		{"SeekCurrent +3", 3, io.SeekCurrent, 8},
		{"SeekEnd -5", -5, io.SeekEnd, int64(len(data)) - 5},
		{"SeekStart 0", 0, io.SeekStart, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fr.Seek(tt.offset, tt.whence)
			if err != nil {
				t.Errorf("Seek() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Seek() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExtractedFileFree(t *testing.T) {
	var buf bytes.Buffer
	ef := NewExtractedFile(&buf)

	// Free should not panic
	ef.Free()

	// Double free should not panic
	ef.Free()
}

// Integration Tests

func TestFullWorkflow(t *testing.T) {
	// Open archive
	archive, err := OpenArchiveFromPath(testdataPath(t, "multiple_files.rar"))
	if err != nil {
		t.Fatalf("failed to open archive: %v", err)
	}
	defer archive.Free()

	// Get file count
	count := archive.GetFileCount()
	if count != 3 {
		t.Fatalf("expected 3 files, got %d", count)
	}

	// List all files and verify metadata
	expectedFiles := []struct {
		name string
		size int64
	}{
		{"a.txt", 9},
		{"b.txt", 9},
		{"c.txt", 9},
	}

	for i, exp := range expectedFiles {
		idx := int64(i)

		// Get filename
		name, err := archive.GetFilename(idx)
		if err != nil {
			t.Errorf("GetFilename(%d) error = %v", idx, err)
			continue
		}
		if name != exp.name {
			t.Errorf("file %d: name = %q, want %q", idx, name, exp.name)
		}

		// Check it's not a directory
		if archive.FileIsDirectory(idx) {
			t.Errorf("file %d: unexpectedly marked as directory", idx)
		}

		// Check file is supported
		if err := archive.FileIsSupported(idx); err != nil {
			t.Errorf("file %d: FileIsSupported() = %v", idx, err)
		}

		// Get file stat
		stat := archive.GetFileStat(idx)
		if stat == nil {
			t.Errorf("file %d: GetFileStat() returned nil", idx)
			continue
		}
		if size := stat.GetUncompressedSize(); size != exp.size {
			t.Errorf("file %d: size = %d, want %d", idx, size, exp.size)
		}
	}

	// Extract all files and verify content
	expectedContent := []string{"Content A", "Content B", "Content C"}
	for i, want := range expectedContent {
		var buf bytes.Buffer
		ef := NewExtractedFile(&buf)

		err := archive.ExtractFile(ef, int64(i))
		ef.Free()

		if err != nil {
			t.Errorf("ExtractFile(%d) error = %v", i, err)
			continue
		}

		if got := buf.String(); got != want {
			t.Errorf("file %d: content = %q, want %q", i, got, want)
		}
	}
}

func TestOpenArchive_FromBytesReader(t *testing.T) {
	// Read archive into memory
	path := testdataPath(t, "compressed.rar")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Open from bytes.Reader
	reader := bytes.NewReader(data)
	archive, err := OpenArchive(reader, int64(len(data)))
	if err != nil {
		t.Fatalf("OpenArchive() error = %v", err)
	}
	defer archive.Free()

	// Extract and verify
	content := extractToBytes(t, archive, 0)
	want := "Hello, World!"
	if string(content) != want {
		t.Errorf("extracted = %q, want %q", string(content), want)
	}
}
