package goja

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/spf13/afero"
	mwstorage "gopkg.d7z.net/middleware/storage"
)

const timeLayoutRFC3339Milli = "2006-01-02T15:04:05.000Z07:00"

type storageInfoSnapshot struct {
	Name        string
	Path        string
	Size        int64
	Mode        int64
	ModTime     string
	IsFile      bool
	IsDirectory bool
}

type storageDirentSnapshot struct {
	Name        string
	Path        string
	IsFile      bool
	IsDirectory bool
}

type storageWriteOptions struct {
	encoding string
	append   bool
	mkdir    bool
	mode     os.FileMode
}

type storageMkdirOptions struct {
	recursive bool
	mode      os.FileMode
}

type storageReaddirOptions struct {
	withFileTypes bool
	recursive     bool
}

type storageRmOptions struct {
	recursive bool
	force     bool
}

func newStorageAPI(vm *goja.Runtime, loop *eventloop.EventLoop, runtime *runtimeState, store mwstorage.Storage) *goja.Object {
	obj := vm.NewObject()

	_ = obj.Set("child", func(args ...goja.Value) goja.Value {
		parts := make([]string, 0, len(args))
		for _, arg := range args {
			parts = append(parts, arg.String())
		}
		return newStorageAPI(vm, loop, runtime, store.Child(parts...))
	})

	_ = obj.Set("access", func(target string) *goja.Promise {
		normalized, err := storagePath(target, true)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		return storageAsyncVoid(vm, loop, runtime, func() error {
			_, err := store.Stat(normalized)
			return err
		})
	})
	_ = obj.Set("accessSync", func(target string) error {
		normalized, err := storagePath(target, true)
		if err != nil {
			return err
		}
		_, err = store.Stat(normalized)
		return err
	})

	_ = obj.Set("exists", func(target string) *goja.Promise {
		normalized, err := storagePath(target, true)
		if err != nil {
			promise, resolve, _ := vm.NewPromise()
			_ = resolve(false)
			return promise
		}
		return storageAsyncValue(vm, loop, runtime, func() (bool, error) {
			_, err := store.Stat(normalized)
			if err == nil {
				return true, nil
			}
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, mwstorage.ErrInvalidPath) {
				return false, nil
			}
			return false, err
		}, func(vm *goja.Runtime, exists bool) goja.Value {
			return vm.ToValue(exists)
		})
	})
	_ = obj.Set("existsSync", func(target string) bool {
		normalized, err := storagePath(target, true)
		if err != nil {
			return false
		}
		_, err = store.Stat(normalized)
		return err == nil
	})

	_ = obj.Set("stat", func(target string) *goja.Promise {
		normalized, err := storagePath(target, true)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		return storageAsyncValue(vm, loop, runtime, func() (storageInfoSnapshot, error) {
			info, err := store.Stat(normalized)
			if err != nil {
				return storageInfoSnapshot{}, err
			}
			return snapshotStorageInfo(normalized, info), nil
		}, func(vm *goja.Runtime, info storageInfoSnapshot) goja.Value {
			return storageInfoValue(vm, info)
		})
	})
	_ = obj.Set("statSync", func(target string) (goja.Value, error) {
		normalized, err := storagePath(target, true)
		if err != nil {
			return nil, err
		}
		info, err := store.Stat(normalized)
		if err != nil {
			return nil, err
		}
		return storageInfoValue(vm, snapshotStorageInfo(normalized, info)), nil
	})
	_ = obj.Set("lstat", obj.Get("stat"))
	_ = obj.Set("lstatSync", obj.Get("statSync"))

	_ = obj.Set("readdir", func(targetValue, optionsValue goja.Value) *goja.Promise {
		normalized, err := storageOptionalPath(targetValue, true)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		options, err := parseStorageReaddirOptions(vm, optionsValue)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		return storageAsyncValue(vm, loop, runtime, func() ([]storageDirentSnapshot, error) {
			return storageReadDir(store, normalized, options.recursive)
		}, func(vm *goja.Runtime, entries []storageDirentSnapshot) goja.Value {
			return storageReaddirValue(vm, entries, options)
		})
	})
	_ = obj.Set("readdirSync", func(targetValue, optionsValue goja.Value) (goja.Value, error) {
		normalized, err := storageOptionalPath(targetValue, true)
		if err != nil {
			return nil, err
		}
		options, err := parseStorageReaddirOptions(vm, optionsValue)
		if err != nil {
			return nil, err
		}
		entries, err := storageReadDir(store, normalized, options.recursive)
		if err != nil {
			return nil, err
		}
		return storageReaddirValue(vm, entries, options), nil
	})

	_ = obj.Set("readFile", func(targetValue, optionsValue goja.Value) *goja.Promise {
		target, err := storageRequiredPath(targetValue, false)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		encoding, err := parseStorageEncoding(vm, optionsValue)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		return storageAsyncValue(vm, loop, runtime, func() ([]byte, error) {
			file, err := store.Open(target)
			if err != nil {
				return nil, err
			}
			defer file.Close()
			return io.ReadAll(file)
		}, func(vm *goja.Runtime, data []byte) goja.Value {
			if encoding == "utf8" {
				return vm.ToValue(string(data))
			}
			return uint8ArrayValue(vm, data)
		})
	})
	_ = obj.Set("readFileSync", func(targetValue, optionsValue goja.Value) (goja.Value, error) {
		target, err := storageRequiredPath(targetValue, false)
		if err != nil {
			return nil, err
		}
		encoding, err := parseStorageEncoding(vm, optionsValue)
		if err != nil {
			return nil, err
		}
		file, err := store.Open(target)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			return nil, err
		}
		if encoding == "utf8" {
			return vm.ToValue(string(data)), nil
		}
		return uint8ArrayValue(vm, data), nil
	})

	_ = obj.Set("writeFile", func(targetValue, dataValue, optionsValue goja.Value) *goja.Promise {
		target, body, err := storageWriteInput(vm, targetValue, dataValue)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		options, err := parseStorageWriteOptions(vm, optionsValue)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		return storageAsyncVoid(vm, loop, runtime, func() error {
			return storageWriteFile(store, target, body, options)
		})
	})
	_ = obj.Set("writeFileSync", func(targetValue, dataValue, optionsValue goja.Value) error {
		target, body, err := storageWriteInput(vm, targetValue, dataValue)
		if err != nil {
			return err
		}
		options, err := parseStorageWriteOptions(vm, optionsValue)
		if err != nil {
			return err
		}
		return storageWriteFile(store, target, body, options)
	})

	_ = obj.Set("appendFile", func(targetValue, dataValue, optionsValue goja.Value) *goja.Promise {
		target, body, err := storageWriteInput(vm, targetValue, dataValue)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		options, err := parseStorageWriteOptions(vm, optionsValue)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		options.append = true
		return storageAsyncVoid(vm, loop, runtime, func() error {
			return storageWriteFile(store, target, body, options)
		})
	})
	_ = obj.Set("appendFileSync", func(targetValue, dataValue, optionsValue goja.Value) error {
		target, body, err := storageWriteInput(vm, targetValue, dataValue)
		if err != nil {
			return err
		}
		options, err := parseStorageWriteOptions(vm, optionsValue)
		if err != nil {
			return err
		}
		options.append = true
		return storageWriteFile(store, target, body, options)
	})

	_ = obj.Set("mkdir", func(targetValue, optionsValue goja.Value) *goja.Promise {
		target, err := storageRequiredPath(targetValue, true)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		options, err := parseStorageMkdirOptions(vm, optionsValue)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		return storageAsyncVoid(vm, loop, runtime, func() error {
			if target == "." {
				return nil
			}
			if options.recursive {
				return store.MkdirAll(target, options.mode)
			}
			return store.Mkdir(target, options.mode)
		})
	})
	_ = obj.Set("mkdirSync", func(targetValue, optionsValue goja.Value) error {
		target, err := storageRequiredPath(targetValue, true)
		if err != nil {
			return err
		}
		options, err := parseStorageMkdirOptions(vm, optionsValue)
		if err != nil {
			return err
		}
		if target == "." {
			return nil
		}
		if options.recursive {
			return store.MkdirAll(target, options.mode)
		}
		return store.Mkdir(target, options.mode)
	})

	_ = obj.Set("rm", func(targetValue, optionsValue goja.Value) *goja.Promise {
		target, err := storageRequiredPath(targetValue, true)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		options, err := parseStorageRmOptions(vm, optionsValue)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		return storageAsyncVoid(vm, loop, runtime, func() error {
			return storageRemove(store, target, options)
		})
	})
	_ = obj.Set("rmSync", func(targetValue, optionsValue goja.Value) error {
		target, err := storageRequiredPath(targetValue, true)
		if err != nil {
			return err
		}
		options, err := parseStorageRmOptions(vm, optionsValue)
		if err != nil {
			return err
		}
		return storageRemove(store, target, options)
	})

	_ = obj.Set("unlink", func(target string) *goja.Promise {
		normalized, err := storagePath(target, false)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		return storageAsyncVoid(vm, loop, runtime, func() error {
			return storageUnlink(store, normalized)
		})
	})
	_ = obj.Set("unlinkSync", func(target string) error {
		normalized, err := storagePath(target, false)
		if err != nil {
			return err
		}
		return storageUnlink(store, normalized)
	})

	_ = obj.Set("rename", func(oldValue, newValue, optionsValue goja.Value) *goja.Promise {
		oldPath, newPath, err := storageRequiredPathPair(oldValue, newValue, false, "oldPath and newPath are required")
		if err != nil {
			return rejectedPromise(vm, err)
		}
		if overwrite, ok := storageRenameOverwrite(vm, optionsValue); ok && overwrite {
			return rejectedPromise(vm, errors.New("rename overwrite is not supported"))
		}
		return storageAsyncVoid(vm, loop, runtime, func() error {
			return store.Rename(oldPath, newPath)
		})
	})
	_ = obj.Set("renameSync", func(oldValue, newValue, optionsValue goja.Value) error {
		oldPath, newPath, err := storageRequiredPathPair(oldValue, newValue, false, "oldPath and newPath are required")
		if err != nil {
			return err
		}
		if overwrite, ok := storageRenameOverwrite(vm, optionsValue); ok && overwrite {
			return errors.New("rename overwrite is not supported")
		}
		return store.Rename(oldPath, newPath)
	})

	_ = obj.Set("copyFile", func(srcValue, destValue goja.Value) *goja.Promise {
		src, dest, err := storageRequiredPathPair(srcValue, destValue, false, "src and dest are required")
		if err != nil {
			return rejectedPromise(vm, err)
		}
		return storageAsyncVoid(vm, loop, runtime, func() error {
			return storageCopyFile(store, src, dest)
		})
	})
	_ = obj.Set("copyFileSync", func(src, dest string) error {
		sourcePath, err := storagePath(src, false)
		if err != nil {
			return err
		}
		destPath, err := storagePath(dest, false)
		if err != nil {
			return err
		}
		return storageCopyFile(store, sourcePath, destPath)
	})

	return obj
}

func storageAsyncVoid(vm *goja.Runtime, loop *eventloop.EventLoop, runtime *runtimeState, work func() error) *goja.Promise {
	return storageAsyncValue(vm, loop, runtime, func() (struct{}, error) {
		return struct{}{}, work()
	}, func(vm *goja.Runtime, _ struct{}) goja.Value {
		return goja.Undefined()
	})
}

func storageAsyncValue[T any](vm *goja.Runtime, loop *eventloop.EventLoop, runtime *runtimeState, work func() (T, error), toValue func(*goja.Runtime, T) goja.Value) *goja.Promise {
	promise, resolve, reject := vm.NewPromise()
	if !runtime.startTask() {
		_ = reject(vm.ToValue(errRuntimeClosing))
		return promise
	}
	go func() {
		defer runtime.finishTask()
		result, err := work()
		runtime.runOnLoop(loop, func(vm *goja.Runtime) {
			if err != nil {
				_ = reject(vm.ToValue(err))
				return
			}
			_ = resolve(toValue(vm, result))
		})
	}()
	return promise
}

func rejectedPromise(vm *goja.Runtime, err error) *goja.Promise {
	promise, _, reject := vm.NewPromise()
	_ = reject(vm.ToValue(err))
	return promise
}

func storageRequiredPath(value goja.Value, allowRoot bool) (string, error) {
	if isNilish(value) {
		return "", errors.New("path is required")
	}
	return storagePath(value.String(), allowRoot)
}

func storageOptionalPath(value goja.Value, allowRoot bool) (string, error) {
	if isNilish(value) {
		return ".", nil
	}
	return storagePath(value.String(), allowRoot)
}

func storageRequiredPathPair(first, second goja.Value, allowRoot bool, missingErr string) (string, string, error) {
	if isNilish(first) || isNilish(second) {
		return "", "", errors.New(missingErr)
	}
	firstPath, err := storagePath(first.String(), allowRoot)
	if err != nil {
		return "", "", err
	}
	secondPath, err := storagePath(second.String(), allowRoot)
	if err != nil {
		return "", "", err
	}
	return firstPath, secondPath, nil
}

func storageWriteInput(vm *goja.Runtime, targetValue, dataValue goja.Value) (string, []byte, error) {
	if isNilish(targetValue) || isNilish(dataValue) {
		return "", nil, errors.New("path and data are required")
	}
	target, err := storagePath(targetValue.String(), false)
	if err != nil {
		return "", nil, err
	}
	body, err := bodyBytesFromValue(vm, dataValue)
	if err != nil {
		return "", nil, err
	}
	return target, body, nil
}

func storageReaddirValue(vm *goja.Runtime, entries []storageDirentSnapshot, options storageReaddirOptions) goja.Value {
	values := make([]any, 0, len(entries))
	for _, entry := range entries {
		if options.withFileTypes {
			values = append(values, storageDirentValue(vm, entry))
			continue
		}
		if options.recursive {
			values = append(values, entry.Path)
		} else {
			values = append(values, entry.Name)
		}
	}
	return vm.ToValue(values)
}

func parseStorageEncoding(vm *goja.Runtime, value goja.Value) (string, error) {
	if isNilish(value) {
		return "", nil
	}
	if exported, ok := value.Export().(string); ok {
		if exported == "utf8" {
			return exported, nil
		}
		return "", errors.New("unsupported encoding")
	}
	obj, ok := valueObject(vm, value)
	if !ok {
		return "", errors.New("unsupported encoding")
	}
	encoding, ok := objectString(obj, "encoding")
	if !ok || encoding == "" {
		return "", nil
	}
	if encoding != "utf8" {
		return "", errors.New("unsupported encoding")
	}
	return encoding, nil
}

func parseStorageWriteOptions(vm *goja.Runtime, value goja.Value) (storageWriteOptions, error) {
	options := storageWriteOptions{mode: 0o644}
	if isNilish(value) {
		return options, nil
	}
	obj, ok := valueObject(vm, value)
	if !ok {
		return options, errors.New("invalid write options")
	}
	if encoding, ok := objectString(obj, "encoding"); ok {
		if encoding != "" && encoding != "utf8" {
			return options, errors.New("unsupported encoding")
		}
		options.encoding = encoding
	}
	if appendMode, ok := objectBool(obj, "append"); ok {
		options.append = appendMode
	}
	if mkdir, ok := objectBool(obj, "mkdir"); ok {
		options.mkdir = mkdir
	}
	if mode, ok := objectInt64(obj, "mode"); ok && mode > 0 {
		options.mode = os.FileMode(mode)
	}
	return options, nil
}

func parseStorageMkdirOptions(vm *goja.Runtime, value goja.Value) (storageMkdirOptions, error) {
	options := storageMkdirOptions{mode: 0o755}
	if isNilish(value) {
		return options, nil
	}
	obj, ok := valueObject(vm, value)
	if !ok {
		return options, errors.New("invalid mkdir options")
	}
	if recursive, ok := objectBool(obj, "recursive"); ok {
		options.recursive = recursive
	}
	if mode, ok := objectInt64(obj, "mode"); ok && mode > 0 {
		options.mode = os.FileMode(mode)
	}
	return options, nil
}

func parseStorageReaddirOptions(vm *goja.Runtime, value goja.Value) (storageReaddirOptions, error) {
	options := storageReaddirOptions{}
	if isNilish(value) {
		return options, nil
	}
	obj, ok := valueObject(vm, value)
	if !ok {
		return options, errors.New("invalid readdir options")
	}
	if withFileTypes, ok := objectBool(obj, "withFileTypes"); ok {
		options.withFileTypes = withFileTypes
	}
	if recursive, ok := objectBool(obj, "recursive"); ok {
		options.recursive = recursive
	}
	return options, nil
}

func parseStorageRmOptions(vm *goja.Runtime, value goja.Value) (storageRmOptions, error) {
	options := storageRmOptions{}
	if isNilish(value) {
		return options, nil
	}
	obj, ok := valueObject(vm, value)
	if !ok {
		return options, errors.New("invalid rm options")
	}
	if recursive, ok := objectBool(obj, "recursive"); ok {
		options.recursive = recursive
	}
	if force, ok := objectBool(obj, "force"); ok {
		options.force = force
	}
	return options, nil
}

func storageRenameOverwrite(vm *goja.Runtime, value goja.Value) (bool, bool) {
	obj, ok := valueObject(vm, value)
	if !ok {
		return false, false
	}
	return objectBool(obj, "overwrite")
}

func storageWriteFile(store mwstorage.Storage, target string, body []byte, options storageWriteOptions) error {
	if options.mkdir {
		parent := path.Dir(target)
		if parent != "." {
			if err := store.MkdirAll(parent, 0o755); err != nil {
				return err
			}
		}
	}
	flags := os.O_CREATE | os.O_WRONLY
	if options.append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := store.OpenFile(target, flags, options.mode)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(body)
	return err
}

func storageReadDir(store mwstorage.Storage, target string, recursive bool) ([]storageDirentSnapshot, error) {
	if !recursive {
		entries, err := afero.ReadDir(store, target)
		if err != nil {
			return nil, err
		}
		result := make([]storageDirentSnapshot, 0, len(entries))
		for _, entry := range entries {
			entryPath := entry.Name()
			if target != "." {
				entryPath = path.Join(target, entry.Name())
			}
			result = append(result, storageDirentSnapshot{
				Name:        entry.Name(),
				Path:        entryPath,
				IsFile:      !entry.IsDir(),
				IsDirectory: entry.IsDir(),
			})
		}
		return result, nil
	}

	root := target
	result := make([]storageDirentSnapshot, 0)
	err := aferoWalk(store, root, func(current string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if current == root {
			return nil
		}
		result = append(result, storageDirentSnapshot{
			Name:        info.Name(),
			Path:        current,
			IsFile:      !info.IsDir(),
			IsDirectory: info.IsDir(),
		})
		return nil
	})
	return result, err
}

func storageRemove(store mwstorage.Storage, target string, options storageRmOptions) error {
	if options.recursive {
		err := store.RemoveAll(target)
		if options.force && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	err := store.Remove(target)
	if options.force && errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func storageUnlink(store mwstorage.Storage, target string) error {
	info, err := store.Stat(target)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return &os.PathError{Op: "unlink", Path: target, Err: fs.ErrInvalid}
	}
	return store.Remove(target)
}

func storageCopyFile(store mwstorage.Storage, src, dest string) error {
	in, err := store.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	return storageWriteFile(store, dest, data, storageWriteOptions{mode: 0o644})
}

func snapshotStorageInfo(target string, info os.FileInfo) storageInfoSnapshot {
	return storageInfoSnapshot{
		Name:        info.Name(),
		Path:        target,
		Size:        info.Size(),
		Mode:        int64(info.Mode()),
		ModTime:     info.ModTime().Format(timeLayoutRFC3339Milli),
		IsFile:      !info.IsDir(),
		IsDirectory: info.IsDir(),
	}
}

func storageInfoValue(vm *goja.Runtime, info storageInfoSnapshot) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("name", info.Name)
	_ = obj.Set("path", info.Path)
	_ = obj.Set("size", info.Size)
	_ = obj.Set("mode", info.Mode)
	_ = obj.Set("modTime", info.ModTime)
	_ = obj.Set("isFile", func() bool { return info.IsFile })
	_ = obj.Set("isDirectory", func() bool { return info.IsDirectory })
	return obj
}

func storageDirentValue(vm *goja.Runtime, entry storageDirentSnapshot) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("name", entry.Name)
	_ = obj.Set("path", entry.Path)
	_ = obj.Set("isFile", func() bool { return entry.IsFile })
	_ = obj.Set("isDirectory", func() bool { return entry.IsDirectory })
	return obj
}

func storagePath(raw string, allowRoot bool) (string, error) {
	raw = strings.ReplaceAll(raw, "\\", "/")
	if raw == "" || raw == "." || raw == "/" {
		if allowRoot {
			return ".", nil
		}
		return "", errors.New("storage root path is not allowed")
	}
	raw = strings.TrimPrefix(raw, "/")
	raw = strings.TrimSuffix(raw, "/")
	if raw == "" {
		if allowRoot {
			return ".", nil
		}
		return "", errors.New("storage root path is not allowed")
	}
	parts := strings.Split(raw, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", mwstorage.ErrInvalidPath
		}
	}
	return path.Join(parts...), nil
}

func aferoWalk(store mwstorage.Storage, root string, walkFn filepathWalkFunc) error {
	info, err := store.Stat(root)
	if err != nil {
		return walkFn(root, nil, err)
	}
	return aferoWalkInner(store, root, info, walkFn)
}

type filepathWalkFunc func(path string, info os.FileInfo, err error) error

func aferoWalkInner(store mwstorage.Storage, current string, info os.FileInfo, walkFn filepathWalkFunc) error {
	if err := walkFn(current, info, nil); err != nil {
		if errors.Is(err, fs.SkipDir) && info.IsDir() {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	entries, err := afero.ReadDir(store, current)
	if err != nil {
		return walkFn(current, info, err)
	}
	for _, entry := range entries {
		childPath := entry.Name()
		if current != "." {
			childPath = path.Join(current, entry.Name())
		}
		if err := aferoWalkInner(store, childPath, entry, walkFn); err != nil {
			return err
		}
	}
	return nil
}
