/*
 * Copyright (c) 2017, MegaEase
 * All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package api

import (
	"net/http"
	"sort"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/megaease/easegress/pkg/logger"
)

type (
	dynamicMux struct {
		server *Server
		done   chan struct{}
		router atomic.Value
	}
)

func newDynamicMux(server *Server) *dynamicMux {
	m := &dynamicMux{
		server: server,
		done:   make(chan struct{}),
	}

	m.router.Store(chi.NewRouter())

	go m.run()

	return m
}

func (m *dynamicMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.router.Load().(*chi.Mux).ServeHTTP(w, r)
}

func (m *dynamicMux) run() {
	for {
		select {
		// api服务关闭之后，退出当前goroutine
		case <-m.done:
			return
		// TODO 订阅api变更事件
		case <-apisChangeChan:
			m.reloadAPIs()
		}
	}
}

func (m *dynamicMux) reloadAPIs() {
	apisMutex.Lock()
	defer apisMutex.Unlock()

	apiGroups := make([]*Group, 0, len(apis))
	for _, group := range apis {
		apiGroups = append(apiGroups, group)
	}

	sort.Sort(apisByOrder(apiGroups))

	router := chi.NewMux()
	// 注册中间件
	router.Use(middleware.StripSlashes)
	router.Use(m.newAPILogger)
	router.Use(m.newConfigVersionAttacher)
	router.Use(m.newRecoverer)

	for _, apiGroup := range apiGroups {
		for _, api := range apiGroup.Entries {
			path := APIPrefix + api.Path

			switch api.Method {
			case "GET":
				router.Get(path, api.Handler)
			case "HEAD":
				router.Head(path, api.Handler)
			case "PUT":
				router.Put(path, api.Handler)
			case "POST":
				router.Post(path, api.Handler)
			case "PATCH":
				router.Patch(path, api.Handler)
			case "DELETE":
				router.Delete(path, api.Handler)
			case "CONNECT":
				router.Connect(path, api.Handler)
			case "OPTIONS":
				router.Options(path, api.Handler)
			case "TRACE":
				router.Trace(path, api.Handler)
			default:
				logger.Errorf("BUG: group %s unsupported method: %s",
					apiGroup.Group, api.Method)
			}
		}
	}

	m.router.Store(router)
}

func (m *dynamicMux) close() {
	close(m.done)
}
