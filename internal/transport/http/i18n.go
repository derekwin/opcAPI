package http

import (
	"net/http"
	"strings"
)

const (
	languageEN   = "en"
	languageZhCN = "zh-CN"
)

var zhCNByCode = map[string]string{
	"api_key_provision_failed":     "租户 API Key 创建失败",
	"audit_unavailable":            "审计服务不可用",
	"billing_unavailable":          "计费服务不可用",
	"daily_token_limit_exceeded":   "当日 token 用量已达上限",
	"forbidden":                    "无权限访问",
	"insufficient_balance":         "余额不足",
	"internal_error":               "服务器内部错误",
	"invalid_api_key":              "API Key 无效",
	"invalid_audit_event":          "审计事件不合法",
	"invalid_auth_request":         "认证请求不合法",
	"invalid_authorization_header": "Authorization 请求头格式无效",
	"invalid_billing_request":      "计费请求不合法",
	"invalid_json":                 "请求体不是合法的 JSON",
	"invalid_policy":               "策略参数不合法",
	"invalid_prompt_api_request":   "Prompt API 请求不合法",
	"invalid_session":              "会话无效",
	"method_not_allowed":           "不支持该请求方法",
	"missing_api_key":              "缺少 Authorization 请求头",
	"missing_input":                "缺少 input 参数",
	"missing_messages":             "缺少 messages 参数",
	"missing_model":                "缺少 model 参数",
	"not_found":                    "资源不存在",
	"owner_required":               "权限不足：需要 owner 角色",
	"policy_not_found":             "策略不存在",
	"policy_sync_unavailable":      "策略同步服务不可用",
	"prompt_api_not_found":         "Prompt API 不存在",
	"prompt_api_not_published":     "Prompt API 未发布",
	"prompt_api_unavailable":       "Prompt API 服务不可用",
	"rpm_limit_exceeded":           "请求频率超出限制",
	"streaming_not_supported":      "当前服务不支持流式响应",
	"tenant_binding_required":      "API Key 未绑定租户",
	"tenant_not_found":             "租户不存在",
	"upstream_error":               "上游模型服务请求失败",
	"user_disabled":                "用户已被禁用",
	"user_exists":                  "用户已存在",
	"user_not_found":               "用户不存在",
	"user_service_unavailable":     "用户服务不可用",
}

var zhCNByMessage = map[string]string{
	"api key is not bound to tenant":            "API Key 未绑定租户",
	"audit service unavailable":                 "审计服务不可用",
	"audit store unavailable":                   "审计存储不可用",
	"billing service unavailable":               "计费服务不可用",
	"billing store unavailable":                 "计费存储不可用",
	"cannot manage keys outside current tenant": "无法管理当前租户之外的密钥",
	"cannot query keys outside current tenant":  "无法查询当前租户之外的密钥",
	"daily token limit exceeded":                "当日 token 用量已达上限",
	"failed to provision tenant api key":        "租户 API Key 创建失败",
	"forbidden":                                 "无权限访问",
	"forbidden: owner required":                 "权限不足：需要 owner 角色",
	"input is required":                         "缺少 input 参数",
	"insufficient balance":                      "余额不足",
	"internal server error":                     "服务器内部错误",
	"invalid amount":                            "金额不合法",
	"invalid API key":                           "API Key 无效",
	"invalid auth channel":                      "登录渠道不合法",
	"invalid Authorization header format":       "Authorization 请求头格式无效",
	"invalid currency":                          "币种不合法",
	"invalid identifier":                        "账号标识不合法",
	"invalid JSON request body":                 "请求体不是合法的 JSON",
	"invalid payment channel":                   "支付渠道不合法",
	"invalid plan":                              "订阅计划不合法",
	"invalid policy":                            "策略参数不合法",
	"invalid prompt api model":                  "Prompt API 模型不合法",
	"invalid prompt api name":                   "Prompt API 名称不合法",
	"invalid prompt api slug":                   "Prompt API 标识不合法",
	"invalid prompt api template":               "Prompt API 模板不合法",
	"invalid prompt input":                      "Prompt 输入不合法",
	"invalid role":                              "角色不合法",
	"invalid session":                           "会话无效",
	"invalid status":                            "状态不合法",
	"invalid subscription request":              "订阅请求不合法",
	"invalid usage":                             "用量参数不合法",
	"invalid verification code":                 "验证码无效",
	"messages is required":                      "缺少 messages 参数",
	"missing Authorization header":              "缺少 Authorization 请求头",
	"model is required":                         "缺少 model 参数",
	"not found":                                 "资源不存在",
	"policy sync unavailable":                   "策略同步服务不可用",
	"prompt api is not published":               "Prompt API 未发布",
	"prompt api not found":                      "Prompt API 不存在",
	"prompt api service unavailable":            "Prompt API 服务不可用",
	"prompt api store unavailable":              "Prompt API 存储不可用",
	"request rate limit exceeded":               "请求频率超出限制",
	"session token expired":                     "会话令牌已过期",
	"session token invalid":                     "会话令牌无效",
	"streaming not supported":                   "当前服务不支持流式响应",
	"tenant not found":                          "租户不存在",
	"unsupported method":                        "不支持该请求方法",
	"upstream provider request failed":          "上游模型服务请求失败",
	"user already exists":                       "用户已存在",
	"user disabled":                             "用户已被禁用",
	"user not found":                            "用户不存在",
	"user service unavailable":                  "用户服务不可用",
	"user store unavailable":                    "用户存储不可用",
}

func localizeMessage(r *http.Request, code, msg string) (language string, localized string) {
	language = detectLanguage(r)
	if language != languageZhCN {
		return languageEN, msg
	}

	if translated, ok := zhCNByCode[strings.TrimSpace(code)]; ok {
		return languageZhCN, translated
	}
	if translated, ok := zhCNByMessage[strings.TrimSpace(msg)]; ok {
		return languageZhCN, translated
	}
	return languageZhCN, msg
}

func detectLanguage(r *http.Request) string {
	if r == nil {
		return languageEN
	}

	if raw := strings.TrimSpace(r.URL.Query().Get("lang")); raw != "" {
		if isChineseLang(raw) {
			return languageZhCN
		}
		if isEnglishLang(raw) {
			return languageEN
		}
	}

	acceptLanguage := strings.TrimSpace(r.Header.Get("Accept-Language"))
	if acceptLanguage == "" {
		return languageEN
	}

	for _, segment := range strings.Split(acceptLanguage, ",") {
		tag := strings.TrimSpace(segment)
		if tag == "" {
			continue
		}
		if idx := strings.Index(tag, ";"); idx >= 0 {
			tag = strings.TrimSpace(tag[:idx])
		}
		if isChineseLang(tag) {
			return languageZhCN
		}
		if isEnglishLang(tag) {
			return languageEN
		}
	}

	return languageEN
}

func isChineseLang(tag string) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	return tag == "zh" || strings.HasPrefix(tag, "zh-")
}

func isEnglishLang(tag string) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	return tag == "en" || strings.HasPrefix(tag, "en-")
}
