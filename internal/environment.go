package webscenario

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/yuin/gopher-lua"
)

var (
	Version = "HEAD"
	Commit  = "UNKNOWN"
)

type Environment struct {
	sync.Mutex // this mutex works like the GIL in Python.

	lua     *lua.LState
	ctx     context.Context
	stop    context.CancelFunc
	tabs    []*Tab
	logger  *Logger
	storage *Storage
	saveWG  sync.WaitGroup
	errch   chan error

	EnableRecording bool
}

func NewEnvironment(ctx context.Context, logger *Logger, s *Storage, arg Arg) *Environment {
	L := lua.NewState()
	ctx, stop := context.WithCancel(ctx)
	L.SetContext(ctx)

	env := &Environment{
		lua:     L,
		ctx:     ctx,
		stop:    stop,
		logger:  logger,
		storage: s,
		errch:   make(chan error, 1),
	}
	env.Lock()

	RegisterLogger(L, logger)
	RegisterElementType(ctx, L)
	RegisterTabType(ctx, env)
	RegisterTime(ctx, env)
	RegisterAssert(L)
	RegisterKey(L)
	RegisterFileLike(L)
	RegisterEncodings(env)
	RegisterFetch(ctx, env)
	s.Register(env)
	arg.Register(L)

	return env
}

func (env *Environment) Close() error {
	defer env.Unlock()
	for _, t := range env.tabs {
		t.Close()
	}
	env.lua.Close()
	env.stop()
	env.saveWG.Wait()
	close(env.errch)
	return nil
}

func HandleError(L *lua.LState, err error) {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			L.RaiseError("timeout")
		} else if errors.Is(err, context.Canceled) {
			L.RaiseError("interrupted")
		} else {
			L.RaiseError("%s", err)
		}
	}
}

func (env *Environment) DoFile(path string) error {
	done := make(chan struct{})

	go func() {
		err := env.lua.DoFile(path)
		if err != nil {
			env.errch <- err
		}
		close(done)
	}()

	var err error
	select {
	case <-done:
	case err = <-env.errch:
		env.stop()
		<-done
		ctx, stop := context.WithCancel(env.ctx)
		env.lua.SetContext(ctx)
		env.stop = stop
	}
	return err
}

// Yield makes a chance to execute callback function.
func (env *Environment) Yield() {
	env.Unlock()
	env.Lock()
}

// AsyncRun makes a chance to execute callback function while executing a heavy function.
func AsyncRun[T any](env *Environment, L *lua.LState, f func() (T, error)) T {
	env.Unlock()
	defer env.Lock()
	x, err := f()
	HandleError(L, err)
	return x
}

// CallEventHandler calls an event callback function with GIL.
func (env *Environment) CallEventHandler(f *lua.LFunction, arg *lua.LTable, nret int) []lua.LValue {
	env.Lock()
	defer env.Unlock()

	L, cancel := env.lua.NewThread()
	defer cancel()

	L.Push(f)
	L.Push(arg)
	env.errch <- L.PCall(1, nret, nil)

	var result []lua.LValue
	for i := 1; i <= nret; i++ {
		result = append(result, L.Get(i))
	}
	return result
}

func (env *Environment) StartTask(where, taskName string) {
	env.logger.StartTask(where, taskName)
}

func (env *Environment) BuildTable(build func(L *lua.LState, tbl *lua.LTable)) *lua.LTable {
	env.Lock()
	defer env.Unlock()
	tbl := env.lua.NewTable()
	build(env.lua, tbl)
	return tbl
}

func (env *Environment) NewFunction(f lua.LGFunction) *lua.LFunction {
	return env.lua.NewFunction(f)
}

func (env *Environment) RegisterFunction(name string, f lua.LGFunction) {
	env.lua.SetGlobal(name, env.NewFunction(f))
}

func (env *Environment) RegisterTable(name string, fields, meta map[string]lua.LValue) {
	tbl := env.lua.NewTable()
	for k, v := range fields {
		env.lua.SetField(tbl, k, v)
	}
	if meta != nil {
		m := env.lua.NewTable()
		for k, v := range meta {
			env.lua.SetField(m, k, v)
		}
		env.lua.SetMetatable(tbl, m)
	}
	env.lua.SetGlobal(name, tbl)
}

func (env *Environment) RegisterNewType(name string, methods map[string]lua.LGFunction, fields map[string]lua.LValue) {
	tbl := env.lua.SetFuncs(env.lua.NewTypeMetatable(name), methods)
	for k, v := range fields {
		env.lua.SetField(tbl, k, v)
	}
	env.lua.SetGlobal(name, tbl)
}

func (env *Environment) saveRecord(id int, recorder *Recorder) {
	env.saveWG.Add(1)
	go func(id int) {
		<-recorder.Done
		if f, err := env.storage.Open(fmt.Sprintf("record%d.gif", id)); err == nil {
			err = recorder.SaveTo(f)
			f.Close()
			if err == NoRecord {
				env.storage.Remove(f.Name())
			}
		}
		env.saveWG.Done()
	}(id)
}

func (env *Environment) registerTab(t *Tab) {
	env.tabs = append(env.tabs, t)
}

func (env *Environment) unregisterTab(t *Tab) {
	tabs := make([]*Tab, 0, len(env.tabs))
	for _, x := range env.tabs {
		if x != t {
			tabs = append(tabs, x)
		}
	}
	env.tabs = tabs
}

func (env *Environment) RecordOnAllTabs(L *lua.LState, taskName string) {
	for _, tab := range env.tabs {
		tab.RecordOnce(L, taskName)
	}
}
