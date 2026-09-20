//go:build !windows

package membership

import (
	"net/http"
	"net/url"
)

// resolveSystemProxy 非 Windows 平台直接使用标准环境变量代理
func resolveSystemProxy(req *http.Request) (*url.URL, error) {
	return http.ProxyFromEnvironment(req)
}

func detectFallbackProxy(req *http.Request) *url.URL {
	return nil
}
