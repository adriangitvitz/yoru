package stdlib

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/adriangitvitz/yoru/interpreter"
)

// FSProvider implements the FS effect namespace.
// When Interp is set, the tracker-aware methods (with_session,
// write_tracked) become usable; otherwise they error with fs_no_interpreter.
type FSProvider struct {
	Interp *interpreter.Interpreter
}

func (p *FSProvider) EffectName() string { return "FS" }

func (p *FSProvider) WithInterp(interp *interpreter.Interpreter) *FSProvider {
	p.Interp = interp
	return p
}

func (p *FSProvider) Methods() map[string]interpreter.Value {
	return map[string]interpreter.Value{
		"read":               builtin("FS.read", p.read),
		"read_bytes":         builtin("FS.read_bytes", p.readBytes),
		"read_lines":         builtin("FS.read_lines", p.readLines),
		"is_binary":          builtin("FS.is_binary", p.isBinary),
		"write":              builtin("FS.write", p.write),
		"write_bytes":        builtin("FS.write_bytes", p.writeBytes),
		"write_with":         builtin("FS.write_with", p.writeWith),
		"exists":             builtin("FS.exists", p.exists),
		"stat":               builtin("FS.stat", p.stat),
		"list":               builtin("FS.list", p.list),
		"list_recursive":     builtin("FS.list_recursive", p.listRecursive),
		"delete":             builtin("FS.delete", p.delete),
		"mkdir":              builtin("FS.mkdir", p.mkdir),
		"copy":               builtin("FS.copy", p.copy),
		"with_session":       builtin("FS.with_session", p.withSession),
		"write_tracked":      builtin("FS.write_tracked", p.writeTracked),
		"write_tracked_with": builtin("FS.write_tracked_with", p.writeTrackedWith),
	}
}

func builtin(name string, fn func([]interpreter.Value) (interpreter.Value, error)) *interpreter.BuiltinVal {
	return &interpreter.BuiltinVal{Name: name, Fn: fn}
}

func (p *FSProvider) read(args []interpreter.Value) (interpreter.Value, error) {
	path, err := strArg(args, 0, "FS.read(path)")
	if err != nil {
		return fsErr("fs_bad_args", err.Error()), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fsErr(mapIOErrKind(err), err.Error()), nil
	}
	if isBinaryContent(data) {
		return fsErr("fs_binary", "file appears to be binary; use FS.read_bytes for raw bytes"), nil
	}
	if sess := p.currentSession(); sess != nil {
		hash := sha256Hex(data)
		if info, err := os.Stat(path); err == nil {
			sess.Record(path, hash, info.ModTime().Unix())
		}
	}
	return &interpreter.StringVal{V: string(data)}, nil
}

func (p *FSProvider) readBytes(args []interpreter.Value) (interpreter.Value, error) {
	path, err := strArg(args, 0, "FS.read_bytes(path)")
	if err != nil {
		return fsErr("fs_bad_args", err.Error()), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fsErr(mapIOErrKind(err), err.Error()), nil
	}
	if sess := p.currentSession(); sess != nil {
		hash := sha256Hex(data)
		if info, err := os.Stat(path); err == nil {
			sess.Record(path, hash, info.ModTime().Unix())
		}
	}
	return &interpreter.BytesVal{V: data}, nil
}

func (p *FSProvider) writeBytes(args []interpreter.Value) (interpreter.Value, error) {
	if len(args) != 2 {
		return fsErr("fs_bad_args", "FS.write_bytes(path, bytes) takes 2 arguments"), nil
	}
	path, ok := args[0].(*interpreter.StringVal)
	if !ok {
		return fsErr("fs_bad_args", "path must be a String"), nil
	}
	data, ok := args[1].(*interpreter.BytesVal)
	if !ok {
		return fsErr("fs_bad_args", "second argument must be Bytes"), nil
	}
	n, _, werr := atomicWrite(path.V, string(data.V))
	if werr != nil {
		var ke fsKindError
		if errors.As(werr, &ke) {
			return fsErr(ke.kind, ke.msg), nil
		}
		return fsErr("fs_io", werr.Error()), nil
	}
	return &interpreter.IntVal{V: int64(n)}, nil
}

func (p *FSProvider) isBinary(args []interpreter.Value) (interpreter.Value, error) {
	path, err := strArg(args, 0, "FS.is_binary(path)")
	if err != nil {
		return fsErr("fs_bad_args", err.Error()), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fsErr(mapIOErrKind(err), err.Error()), nil
	}
	return &interpreter.BoolVal{V: isBinaryContent(data)}, nil
}

// isBinaryContent reports true when data contains a null byte or invalid UTF-8.
func isBinaryContent(data []byte) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}
	return !utf8.Valid(data)
}

// readLines slices lines [offset, offset+limit) from a file; limit=0 means to end.
func (p *FSProvider) readLines(args []interpreter.Value) (interpreter.Value, error) {
	if len(args) != 3 {
		return fsErr("fs_bad_args", "FS.read_lines(path, offset, limit) takes 3 arguments"), nil
	}
	path, ok := args[0].(*interpreter.StringVal)
	if !ok {
		return fsErr("fs_bad_args", "path must be a String"), nil
	}
	offsetV, ok := args[1].(*interpreter.IntVal)
	if !ok {
		return fsErr("fs_bad_args", "offset must be an Int"), nil
	}
	limitV, ok := args[2].(*interpreter.IntVal)
	if !ok {
		return fsErr("fs_bad_args", "limit must be an Int"), nil
	}

	f, err := os.Open(path.V)
	if err != nil {
		return fsErr(mapIOErrKind(err), err.Error()), nil
	}
	defer f.Close()

	offset := int(offsetV.V)
	limit := int(limitV.V)
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var (
		total     int
		picked    []string
		picking   bool
		remaining = limit
	)
	for scanner.Scan() {
		if total == offset {
			picking = true
		}
		if picking && (limit == 0 || remaining > 0) {
			picked = append(picked, scanner.Text())
			if limit > 0 {
				remaining--
			}
		}
		total++
	}
	if err := scanner.Err(); err != nil {
		return fsErr("fs_io", err.Error()), nil
	}

	return &interpreter.ObjectVal{
		TypeName: "FileSlice",
		Fields: map[string]interpreter.Value{
			"content":        &interpreter.StringVal{V: strings.Join(picked, "\n")},
			"total_lines":    &interpreter.IntVal{V: int64(total)},
			"lines_returned": &interpreter.IntVal{V: int64(len(picked))},
			"offset":         &interpreter.IntVal{V: int64(offset)},
		},
	}, nil
}

func (p *FSProvider) write(args []interpreter.Value) (interpreter.Value, error) {
	path, content, err := twoStringArgs(args, "FS.write(path, content)")
	if err != nil {
		return fsErr("fs_bad_args", err.Error()), nil
	}
	n, _, err2 := atomicWrite(path, content)
	if err2 != nil {
		var ke fsKindError
		if errors.As(err2, &ke) {
			return fsErr(ke.kind, ke.msg), nil
		}
		return fsErr("fs_io", err2.Error()), nil
	}
	return &interpreter.IntVal{V: int64(n)}, nil
}

// writeWith accepts opts {backup, no_overwrite} and returns
// {bytes_written, created, backup_path}.
func (p *FSProvider) writeWith(args []interpreter.Value) (interpreter.Value, error) {
	if len(args) != 3 {
		return fsErr("fs_bad_args", "FS.write_with(path, content, opts) takes 3 arguments"), nil
	}
	path, ok := args[0].(*interpreter.StringVal)
	if !ok {
		return fsErr("fs_bad_args", "path must be a String"), nil
	}
	content, ok := args[1].(*interpreter.StringVal)
	if !ok {
		return fsErr("fs_bad_args", "content must be a String"), nil
	}
	opts, ok := args[2].(*interpreter.ObjectVal)
	if !ok {
		return fsErr("fs_bad_args", "opts must be an Object"), nil
	}

	backup := boolField(opts, "backup")
	noOverwrite := boolField(opts, "no_overwrite")

	preExists := fileExists(path.V)
	if noOverwrite && preExists {
		return fsErr("fs_exists", fmt.Sprintf("file already exists: %s", path.V)), nil
	}

	var backupPath string
	if backup && preExists {
		bk, err := copyToBackup(path.V)
		if err != nil {
			return fsErr("fs_io", "backup failed: "+err.Error()), nil
		}
		backupPath = bk
	}

	n, _, err := atomicWrite(path.V, content.V)
	if err != nil {
		var ke fsKindError
		if errors.As(err, &ke) {
			return fsErr(ke.kind, ke.msg), nil
		}
		return fsErr("fs_io", err.Error()), nil
	}

	return &interpreter.ObjectVal{
		TypeName: "WriteResult",
		Fields: map[string]interpreter.Value{
			"bytes_written": &interpreter.IntVal{V: int64(n)},
			"created":       &interpreter.BoolVal{V: !preExists},
			"backup_path":   &interpreter.StringVal{V: backupPath},
		},
	}, nil
}

func (p *FSProvider) exists(args []interpreter.Value) (interpreter.Value, error) {
	path, err := strArg(args, 0, "FS.exists(path)")
	if err != nil {
		return fsErr("fs_bad_args", err.Error()), nil
	}
	return &interpreter.BoolVal{V: fileExists(path)}, nil
}

func (p *FSProvider) stat(args []interpreter.Value) (interpreter.Value, error) {
	path, err := strArg(args, 0, "FS.stat(path)")
	if err != nil {
		return fsErr("fs_bad_args", err.Error()), nil
	}
	info, err2 := os.Stat(path)
	if err2 != nil {
		return fsErr(mapIOErrKind(err2), err2.Error()), nil
	}
	return statToObject(info), nil
}

func (p *FSProvider) list(args []interpreter.Value) (interpreter.Value, error) {
	path, err := strArg(args, 0, "FS.list(path)")
	if err != nil {
		return fsErr("fs_bad_args", err.Error()), nil
	}
	entries, err2 := os.ReadDir(path)
	if err2 != nil {
		return fsErr(mapIOErrKind(err2), err2.Error()), nil
	}
	elems := make([]interpreter.Value, 0, len(entries))
	for _, e := range entries {
		elems = append(elems, &interpreter.ObjectVal{
			TypeName: "DirEntry",
			Fields: map[string]interpreter.Value{
				"name":   &interpreter.StringVal{V: e.Name()},
				"is_dir": &interpreter.BoolVal{V: e.IsDir()},
			},
		})
	}
	return &interpreter.ListVal{Elements: elems}, nil
}

// listRecursive walks max_depth levels below root. max_depth=0 yields
// only direct children (with relative paths).
func (p *FSProvider) listRecursive(args []interpreter.Value) (interpreter.Value, error) {
	if len(args) != 2 {
		return fsErr("fs_bad_args", "FS.list_recursive(path, max_depth) takes 2 arguments"), nil
	}
	rootV, ok := args[0].(*interpreter.StringVal)
	if !ok {
		return fsErr("fs_bad_args", "path must be a String"), nil
	}
	depthV, ok := args[1].(*interpreter.IntVal)
	if !ok {
		return fsErr("fs_bad_args", "max_depth must be an Int"), nil
	}
	root := rootV.V
	maxDepth := int(depthV.V)
	if maxDepth < 0 {
		maxDepth = 0
	}

	if _, err := os.Stat(root); err != nil {
		return fsErr(mapIOErrKind(err), err.Error()), nil
	}

	var elems []interpreter.Value
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		slashes := strings.Count(rel, string(os.PathSeparator))
		if slashes > maxDepth {
			return nil
		}
		elems = append(elems, &interpreter.ObjectVal{
			TypeName: "DirEntry",
			Fields: map[string]interpreter.Value{
				"path":   &interpreter.StringVal{V: rel},
				"is_dir": &interpreter.BoolVal{V: d.IsDir()},
			},
		})
		if d.IsDir() && slashes >= maxDepth {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return fsErr("fs_io", err.Error()), nil
	}
	return &interpreter.ListVal{Elements: elems}, nil
}

func (p *FSProvider) delete(args []interpreter.Value) (interpreter.Value, error) {
	path, err := strArg(args, 0, "FS.delete(path)")
	if err != nil {
		return fsErr("fs_bad_args", err.Error()), nil
	}
	if err := os.Remove(path); err != nil {
		return fsErr(mapIOErrKind(err), err.Error()), nil
	}
	return &interpreter.NilVal{}, nil
}

func (p *FSProvider) mkdir(args []interpreter.Value) (interpreter.Value, error) {
	path, err := strArg(args, 0, "FS.mkdir(path)")
	if err != nil {
		return fsErr("fs_bad_args", err.Error()), nil
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fsErr("fs_io", err.Error()), nil
	}
	return &interpreter.NilVal{}, nil
}

func (p *FSProvider) copy(args []interpreter.Value) (interpreter.Value, error) {
	src, dst, err := twoStringArgs(args, "FS.copy(src, dst)")
	if err != nil {
		return fsErr("fs_bad_args", err.Error()), nil
	}
	n, err2 := copyFile(src, dst)
	if err2 != nil {
		return fsErr(mapIOErrKind(err2), err2.Error()), nil
	}
	return &interpreter.IntVal{V: n}, nil
}

func (p *FSProvider) withSession(args []interpreter.Value) (interpreter.Value, error) {
	if p.Interp == nil {
		return fsErr("fs_no_interpreter", "FS.with_session requires the provider to hold an interpreter handle (use WithInterp)"), nil
	}
	if len(args) != 1 {
		return fsErr("fs_bad_args", "FS.with_session(fn) takes 1 argument"), nil
	}
	switch args[0].(type) {
	case *interpreter.FunctionVal, *interpreter.BuiltinVal:
	default:
		return fsErr("fs_bad_args", "FS.with_session argument must be a function"), nil
	}
	p.Interp.PushFSSession()
	defer p.Interp.PopFSSession()
	return p.Interp.ApplyCallback(args[0], nil), nil
}

func (p *FSProvider) writeTracked(args []interpreter.Value) (interpreter.Value, error) {
	path, content, err := twoStringArgs(args, "FS.write_tracked(path, content)")
	if err != nil {
		return fsErr("fs_bad_args", err.Error()), nil
	}
	if pre := p.precheckTracked(path); pre != nil {
		return pre, nil
	}
	n, _, werr := atomicWrite(path, content)
	if werr != nil {
		return fsErr("fs_io", werr.Error()), nil
	}
	return &interpreter.IntVal{V: int64(n)}, nil
}

func (p *FSProvider) writeTrackedWith(args []interpreter.Value) (interpreter.Value, error) {
	if len(args) != 3 {
		return fsErr("fs_bad_args", "FS.write_tracked_with(path, content, opts) takes 3 arguments"), nil
	}
	path, ok := args[0].(*interpreter.StringVal)
	if !ok {
		return fsErr("fs_bad_args", "path must be a String"), nil
	}
	content, ok := args[1].(*interpreter.StringVal)
	if !ok {
		return fsErr("fs_bad_args", "content must be a String"), nil
	}
	opts, ok := args[2].(*interpreter.ObjectVal)
	if !ok {
		return fsErr("fs_bad_args", "opts must be an Object"), nil
	}
	if pre := p.precheckTracked(path.V); pre != nil {
		return pre, nil
	}
	return p.writeWith([]interpreter.Value{path, content, opts})
}

// precheckTracked returns Result.Err when the tracker rejects the write,
// or nil to proceed.
func (p *FSProvider) precheckTracked(path string) interpreter.Value {
	sess := p.currentSession()
	if sess == nil {
		return fsErr("fs_no_session", "FS.write_tracked requires an active FS.with_session block")
	}
	rec, ok := sess.Get(path)
	if !ok {
		return fsErr("fs_not_read", "path was not read in this session: "+path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fsErr("fs_stale_read", "could not re-read tracked path: "+err.Error())
	}
	if sha256Hex(data) != rec.Hash {
		return fsErr("fs_stale_read", "file changed on disk since it was read: "+path)
	}
	return nil
}

func (p *FSProvider) currentSession() *interpreter.FSSession {
	if p.Interp == nil {
		return nil
	}
	return p.Interp.CurrentFSSession()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type fsKindError struct {
	kind, msg string
}

func (e fsKindError) Error() string { return e.msg }

// atomicWrite writes to a sibling tempfile then renames over the target.
// Parent directories are created as needed.
func atomicWrite(path, content string) (int, bool, error) {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return 0, false, err
		}
	}

	suffix, err := randomHex(8)
	if err != nil {
		return 0, false, err
	}
	tmp := filepath.Join(dir, "."+filepath.Base(path)+"."+suffix+".tmp")

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, false, err
	}
	n, werr := f.WriteString(content)
	cerr := f.Close()
	if werr != nil {
		_ = os.Remove(tmp)
		return 0, false, werr
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return 0, false, cerr
	}

	preExists := fileExists(path)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, false, err
	}
	return n, !preExists, nil
}

func copyToBackup(path string) (string, error) {
	bk := path + ".bak"
	_, err := copyFile(path, bk)
	if err != nil {
		return "", err
	}
	return bk, nil
}

func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	if dir := filepath.Dir(dst); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return 0, err
		}
	}
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	n, err := io.Copy(out, in)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func mapIOErrKind(err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return "fs_not_found"
	}
	if errors.Is(err, fs.ErrExist) {
		return "fs_exists"
	}
	if errors.Is(err, fs.ErrPermission) {
		return "fs_permission"
	}
	return "fs_io"
}

func statToObject(info fs.FileInfo) *interpreter.ObjectVal {
	return &interpreter.ObjectVal{
		TypeName: "FileStat",
		Fields: map[string]interpreter.Value{
			"name":          &interpreter.StringVal{V: info.Name()},
			"size":          &interpreter.IntVal{V: info.Size()},
			"is_dir":        &interpreter.BoolVal{V: info.IsDir()},
			"is_file":       &interpreter.BoolVal{V: info.Mode().IsRegular()},
			"modified_unix": &interpreter.IntVal{V: info.ModTime().Unix()},
		},
	}
}

func strArg(args []interpreter.Value, i int, sig string) (string, error) {
	if len(args) <= i {
		return "", fmt.Errorf("%s missing argument %d", sig, i)
	}
	s, ok := args[i].(*interpreter.StringVal)
	if !ok {
		return "", fmt.Errorf("%s argument %d must be a String", sig, i)
	}
	return s.V, nil
}

func twoStringArgs(args []interpreter.Value, sig string) (string, string, error) {
	if len(args) != 2 {
		return "", "", fmt.Errorf("%s takes 2 arguments, got %d", sig, len(args))
	}
	a, ok := args[0].(*interpreter.StringVal)
	if !ok {
		return "", "", fmt.Errorf("%s: first argument must be a String", sig)
	}
	b, ok := args[1].(*interpreter.StringVal)
	if !ok {
		return "", "", fmt.Errorf("%s: second argument must be a String", sig)
	}
	return a.V, b.V, nil
}

func boolField(obj *interpreter.ObjectVal, name string) bool {
	v, ok := obj.Fields[name]
	if !ok {
		return false
	}
	b, ok := v.(*interpreter.BoolVal)
	if !ok {
		return false
	}
	return b.V
}

func fsErr(kind, message string) interpreter.Value {
	return &interpreter.EnumVal{
		TypeName: "Result",
		Variant:  "Err",
		Fields: map[string]interpreter.Value{
			"error": &interpreter.ObjectVal{
				TypeName: "Error",
				Fields: map[string]interpreter.Value{
					"kind":    &interpreter.StringVal{V: kind},
					"message": &interpreter.StringVal{V: message},
				},
			},
		},
	}
}
