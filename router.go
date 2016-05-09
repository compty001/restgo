package grest

import (
	"net/http"
	"strings"
	"encoding/json"
	"net/url"
	"github.com/valyala/fasthttp"
	"reflect"
)

type (
	Next func(err error)

	HTTPHandler func(ctx *fasthttp.RequestCtx, next Next)

	Router struct {
		stack        []*layer
		routerPrefix string // prefix path, trimmed off it when route
	}
)

// Create one new Router
func NewRouter() *Router {
	router := &Router{
		stack: make([]*layer, 0),
	}

	return router
}

// set handlers for `path`, default is `/`. you can use it as filters
func (this *Router) Use(path string, handlers ...interface{}) *Router {
	if path == "" {
		path = "/" // default to root path
	}


	for _, handler := range handlers {
		var l *layer
		switch handler.(type) {
		case *Router:
			if router, ok := handler.(*Router); ok {
				router.routerPrefix = this.routerPrefix + path // prepare router prefix path
				l = newLayer(path, router.HTTPHandler, false)
			}
		case *Route:
			if route, ok := handler.(*Route); ok {
				l = newLayer(path, route.HTTPHandler, false)
			}
		default:
			fn := reflect.ValueOf(handler)
			fnType := fn.Type()
			if fnType.Kind() != reflect.Func || fnType.NumIn() != 2 || fnType.NumOut() != 0 {
				panic("Expected a type grest.HTTPHandler function")
			}
			l = newLayer(path, func (ctx *fasthttp.RequestCtx, next Next) {
				fn.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(next)})
			}, false)
		}
		if l != nil {
			l.route = nil
			this.stack = append(this.stack, l)
		}
	}

	return this
}

// create a sub-route
func (this *Router) Route(path string) *Route {
	route := newRoute(path)
	l := newLayer(path, route.HTTPHandler, true)

	l.route = route

	this.stack = append(this.stack, l)

	return route
}

// set handlers for all types requests
func (this *Router)All(path string, handlers ...HTTPHandler) *Router {
	this.Route(path).All(handlers...)

	return this
}

func (this *Router) addHandler(method string, path string, handlers ...HTTPHandler) *Router {
	route := this.Route(path)

	switch method {
	case "GET":
		route.GET(handlers...);
	case "POST":
		route.POST(handlers...);
	case "PUT":
		route.PUT(handlers...);
	case "DELETE":
		route.DELETE(handlers...);
	case "HEAD":
		route.HEAD(handlers...);
	// ignore others
	}
	return this
}

// set handlers for `GET` request
func (this *Router) GET(path string, handlers ...HTTPHandler) *Router {
	return this.addHandler("GET", path, handlers...)
}

// set handlers for `POST` request
func (this *Router) POST(path string, handlers ...HTTPHandler) *Router {
	return this.addHandler("POST", path, handlers...)
}

// set handlers for `PUT` request
func (this *Router) PUT(path string, handlers ...HTTPHandler) *Router {
	return this.addHandler("PUT", path, handlers...)
}

// set handlers for `DELETE` request
func (this *Router) DELETE(path string, handlers ...HTTPHandler) *Router {
	return this.addHandler("DELETE", path, handlers...)
}

// set handlers for `HEAD` request
func (this *Router) HEAD(path string, handlers ...HTTPHandler) *Router {
	return this.addHandler("HEAD", path, handlers...)
}

func (this *Router) matchLayer(l *layer, path string) (url.Values, bool) {
	urlParams, match := l.match(path)
	return urlParams, match
}

func (this *Router) route(ctx *fasthttp.RequestCtx, done Next) {
	var next func(err error)
	var idx = 0

	var allowOptionsMethods = make([]string, 0, 5)
	if string(ctx.Method()) == "OPTIONS" {
		// reply OPTIONS request automatically
		old := done
		done = func(err error) {
			if err != nil || len(allowOptionsMethods) == 0 {
				old(err)
			} else {
				ctx.Response.Header.Add("Allow", strings.Join(allowOptionsMethods, ","))
				data, err := json.Marshal(allowOptionsMethods)
				if err != nil {
					old(err)
					return
				}
				ctx.Write(data)
			}

		}
	}

	next = func(err error) {
		if idx >= len(this.stack) {
			done(err)
			return
		}
		// get trimmed path for current router
		path := strings.TrimPrefix(string(ctx.Path()), this.routerPrefix)
		if path == "" {
			done(err)
			return
		}

		// find next matching layer
		var match = false
		var l *layer
		var route *Route
		var urlParams url.Values

		for ; match != true && idx < len(this.stack); {
			l = this.stack[idx]
			idx ++
			urlParams, match = this.matchLayer(l, path);
			route = l.route

			if match != true || route == nil {
				continue
			}
			method := string(ctx.Method())
			hasMethod := route.handlesMethod(method)

			if !hasMethod && method == "OPTIONS" {
				for _, method := range route.optionsMethods() {
					allowOptionsMethods = append(allowOptionsMethods, method)
				}
			}

			if !hasMethod && method != "HEAD" {
				match = false
			}
		}

		if match != true || err != nil {
			done(err)
			return
		}
		l.registerParamsAsQuery(ctx, urlParams)

		l.handleRequest(ctx, next)
	}

	next(nil)
}

// implement HTTPHandler interface, make it can be as a handler
func (this *Router) HTTPHandler(ctx *fasthttp.RequestCtx, next Next) {
	this.route(ctx, next)
}

// implement fasthttp.RequestHandler function
func (this *Router) FastHttpHandler(ctx *fasthttp.RequestCtx) {
	this.route(ctx, func(err error) {
		if err != nil {
			ctx.Error("Something wrong", http.StatusInternalServerError)
			return
		}
		ctx.NotFound()
	})
}



