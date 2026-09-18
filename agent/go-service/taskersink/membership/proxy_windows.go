//go:build windows

package membership

import (
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// resolveSystemProxy 尝试解析适合当前请求的代理服务器。
// 优先使用环境变量中设置的代理；若未配置，则尝试读取 Windows 注册表中的系统代理设置。
func resolveSystemProxy(req *http.Request) (*url.URL, error) {
	if req == nil || req.URL == nil {
		return nil, nil
	}

	// 1. 优先使用标准库环境变量代理 (HTTP_PROXY / HTTPS_PROXY / NO_PROXY)
	if proxyURL, err := http.ProxyFromEnvironment(req); err == nil && proxyURL != nil {
		return proxyURL, nil
	}

	// 2. 读取 Windows 系统代理注册表设置
	k, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return nil, nil
	}
	defer k.Close()

	enable, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || enable == 0 {
		return nil, nil
	}

	serverStr, _, err := k.GetStringValue("ProxyServer")
	if err != nil || strings.TrimSpace(serverStr) == "" {
		return nil, nil
	}

	overrideStr, _, _ := k.GetStringValue("ProxyOverride")
	if isProxyOverridden(req.URL.Hostname(), overrideStr) {
		return nil, nil
	}

	return parseWindowsProxyServer(req.URL.Scheme, serverStr)
}

func isProxyOverridden(hostname string, override string) bool {
	if override == "" || hostname == "" {
		return false
	}
	lowerHost := strings.ToLower(hostname)
	parts := strings.Split(override, ";")
	for _, part := range parts {
		p := strings.TrimSpace(strings.ToLower(part))
		if p == "" {
			continue
		}
		if p == "<local>" {
			// <local> 匹配不含点的主机名或本地回环
			if !strings.Contains(lowerHost, ".") || lowerHost == "localhost" || lowerHost == "127.0.0.1" {
				return true
			}
			continue
		}
		if p == lowerHost {
			return true
		}
		if strings.HasPrefix(p, "*.") && strings.HasSuffix(lowerHost, p[1:]) {
			return true
		}
		if strings.HasPrefix(lowerHost, p) {
			return true
		}
	}
	return false
}

func parseWindowsProxyServer(scheme string, serverStr string) (*url.URL, error) {
	serverStr = strings.TrimSpace(serverStr)
	target := ""

	// 检查是否包含 protocol=host:port 格式
	if strings.Contains(serverStr, "=") {
		entries := strings.Split(serverStr, ";")
		schemePrefix := strings.ToLower(scheme) + "="
		for _, entry := range entries {
			e := strings.TrimSpace(entry)
			if strings.HasPrefix(strings.ToLower(e), schemePrefix) {
				target = e[len(schemePrefix):]
				break
			}
		}
		if target == "" {
			// 如果没有找到特定 scheme 的代理，尝试 http=
			for _, entry := range entries {
				e := strings.TrimSpace(entry)
				if strings.HasPrefix(strings.ToLower(e), "http=") {
					target = e[5:]
					break
				}
			}
		}
	} else {
		target = serverStr
	}

	target = strings.TrimSpace(target)
	if target == "" {
		return nil, nil
	}

	if !strings.Contains(target, "://") {
		target = "http://" + target
	}

	return url.Parse(target)
}
