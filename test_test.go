package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/yuin/gopher-lua"
)

func StartTestServer() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("target")
		if target == "" {
			target = "world"
		}
		fmt.Fprintf(w, `<title>%s - test</title><div id="greeting">hello <b class="target">%s</b>!</div>`, target, target)
	})

	mux.HandleFunc("/complex-dom", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/html")
		fmt.Fprintf(w, `
			<div><h1>text</h1><b>hello </b>beautiful <b>world</b><span>!</span></div>
			<form action=GET><h1>form</h1><input type="text"><input type="text"></form>
		`)
	})

	count := 0
	mux.HandleFunc("/counter", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/html")
		count++
		fmt.Fprintf(w, `current count is <span>%d</span>`, count)
	})

	mux.HandleFunc("/dynamic", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/html")
		fmt.Fprintf(w, `
			<div>
				<ol></ol>
				<script>count = 0</script>
				<button id=append onclick="document.querySelector('ol').innerHTML += '<li>count=' + count + '</li>'; count++;">append</button>
			</div>

			<div>
				<span id="text"></span>
				<input type=text onchange="document.querySelector('#text').innerText = event.target.value">
			</div>

			<div>
				<span id=look-at-me onfocus="event.target.innerText = 'focus'" onblur="event.target.innerText = 'blur'" tabindex=-1>blur</span>
			</div>

			<form>
				<div id=submitted>%s</div>
				<textarea name="textarea"></textarea>
				<input type=submit>
			</form>
		`, r.URL.Query().Get("textarea"))
	})

	mux.HandleFunc("/window-size", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/html")
		fmt.Fprintf(w, `
			<span>loading...</span>
			<script>
				function onResize() {
					document.querySelector('span').innerText = window.innerWidth + 'x' +window.innerHeight;
				}
				window.addEventListener('resize', onResize);
				onResize();
			</script>
		`)
	})

	return httptest.NewServer(mux)
}

func RegisterTestUtil(L *lua.LState, server *httptest.Server) {
	tbl := L.SetFuncs(L.NewTable(), map[string]lua.LGFunction{
		"url": func(L *lua.LState) int {
			L.Push(lua.LString(server.URL + L.OptString(1, "")))
			return 1
		},
	})
	L.SetGlobal("TEST", tbl)
}

func DoLuaLine(L *lua.LState, script string) any {
	L.DoString("return " + script)
	v := UnpackLValue(L.Get(1))
	L.Pop(1)
	return v
}

func AssertLuaLine(t *testing.T, L *lua.LState, script string, want any) {
	t.Helper()

	if diff := cmp.Diff(DoLuaLine(L, script), want); diff != "" {
		t.Errorf("%s\n%s", script, diff)
	}
}

func Test_testSenarios(t *testing.T) {
	t.Setenv("TZ", "UTC")

	files, err := filepath.Glob("testdata/*.lua")
	if err != nil {
		t.Fatalf("failed to get tests: %s", err)
	}

	server := StartTestServer()
	defer server.Close()

	ctx, cancel := NewContext()
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()

	for _, p := range files {
		b := filepath.Base(p)
		if strings.HasPrefix(b, "_") {
			continue
		}
		t.Run(b, func(t *testing.T) {
			logger := &Logger{Debug: true}
			L := NewLuaState(ctx, logger)
			RegisterTestUtil(L, server)

			if err := L.DoFile(p); err != nil {
				t.Fatalf(err.Error())
			}
		})
	}
}
