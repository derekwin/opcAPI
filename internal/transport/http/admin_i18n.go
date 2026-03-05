package http

import (
	"fmt"
	"net/http"
	"strings"
)

func writeLocalizedPlainError(w http.ResponseWriter, r *http.Request, status int, code, fallback string) {
	language, localized := localizeMessage(r, code, fallback)
	w.Header().Set("Content-Language", language)
	http.Error(w, localized, status)
}

func adminFlashMessage(r *http.Request, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	code, detail := splitAdminCodeDetail(raw)
	zh := detectLanguage(r) == languageZhCN
	switch code {
	case "sign_in_success":
		if zh {
			return "登录成功"
		}
		return "Signed in successfully"
	case "signed_out":
		if zh {
			return "已退出登录"
		}
		return "Signed out"
	case "api_key_saved":
		if zh {
			return "API Key 策略已保存"
		}
		return "API key policy saved"
	case "api_key_deleted":
		if zh {
			if detail == "" {
				return "API Key 已删除"
			}
			return fmt.Sprintf("API Key 已删除：%s", detail)
		}
		if detail == "" {
			return "API key deleted"
		}
		return fmt.Sprintf("API key deleted: %s", detail)
	case "topup_success":
		if zh {
			return "充值成功"
		}
		return "Top up succeeded"
	case "order_deleted":
		if zh {
			if detail == "" {
				return "订单已删除"
			}
			return fmt.Sprintf("订单已删除：%s", detail)
		}
		if detail == "" {
			return "Order deleted"
		}
		return fmt.Sprintf("Order deleted: %s", detail)
	case "prompt_api_saved":
		if zh {
			if detail == "" {
				return "Prompt API 已保存"
			}
			return fmt.Sprintf("Prompt API 已保存：%s", detail)
		}
		if detail == "" {
			return "Prompt API saved"
		}
		return fmt.Sprintf("Prompt API saved: %s", detail)
	case "prompt_api_deleted":
		if zh {
			if detail == "" {
				return "Prompt API 已删除"
			}
			return fmt.Sprintf("Prompt API 已删除：%s", detail)
		}
		if detail == "" {
			return "Prompt API deleted"
		}
		return fmt.Sprintf("Prompt API deleted: %s", detail)
	case "default_api_saved":
		if zh {
			if detail == "" {
				return "默认 API 配置已保存"
			}
			return fmt.Sprintf("默认 API 配置已保存：%s", detail)
		}
		if detail == "" {
			return "Default API config saved"
		}
		return fmt.Sprintf("Default API config saved: %s", detail)
	case "user_created":
		if zh {
			if detail == "" {
				return "用户已创建"
			}
			return fmt.Sprintf("用户已创建：%s", detail)
		}
		if detail == "" {
			return "User created"
		}
		return fmt.Sprintf("User created: %s", detail)
	case "user_role_updated":
		if zh {
			if detail == "" {
				return "用户角色已更新"
			}
			return fmt.Sprintf("用户角色已更新：%s", detail)
		}
		if detail == "" {
			return "User role updated"
		}
		return fmt.Sprintf("User role updated: %s", detail)
	case "user_status_updated":
		if zh {
			if detail == "" {
				return "用户状态已更新"
			}
			return fmt.Sprintf("用户状态已更新：%s", detail)
		}
		if detail == "" {
			return "User status updated"
		}
		return fmt.Sprintf("User status updated: %s", detail)
	case "user_balance_updated":
		if zh {
			if detail == "" {
				return "用户余额已更新"
			}
			return fmt.Sprintf("用户余额已更新：%s", detail)
		}
		if detail == "" {
			return "User balance updated"
		}
		return fmt.Sprintf("User balance updated: %s", detail)
	case "user_membership_updated":
		if zh {
			if detail == "" {
				return "用户会员已更新"
			}
			return fmt.Sprintf("用户会员已更新：%s", detail)
		}
		if detail == "" {
			return "User membership updated"
		}
		return fmt.Sprintf("User membership updated: %s", detail)
	case "user_deleted":
		if zh {
			if detail == "" {
				return "用户已删除"
			}
			return fmt.Sprintf("用户已删除：%s", detail)
		}
		if detail == "" {
			return "User deleted"
		}
		return fmt.Sprintf("User deleted: %s", detail)
	case "admin_setup_complete":
		if zh {
			return "管理员初始化完成"
		}
		return "Admin setup complete"
	case "upstream_saved":
		if zh {
			if detail == "" {
				return "上游已保存"
			}
			return fmt.Sprintf("上游已保存：%s", detail)
		}
		if detail == "" {
			return "Upstream saved"
		}
		return fmt.Sprintf("Upstream saved: %s", detail)
	case "upstream_deleted":
		if zh {
			if detail == "" {
				return "上游已删除"
			}
			return fmt.Sprintf("上游已删除：%s", detail)
		}
		if detail == "" {
			return "Upstream deleted"
		}
		return fmt.Sprintf("Upstream deleted: %s", detail)
	case "upstream_enabled":
		if zh {
			if detail == "" {
				return "上游已启用"
			}
			return fmt.Sprintf("上游已启用：%s", detail)
		}
		if detail == "" {
			return "Upstream enabled"
		}
		return fmt.Sprintf("Upstream enabled: %s", detail)
	case "upstream_disabled":
		if zh {
			if detail == "" {
				return "上游已禁用"
			}
			return fmt.Sprintf("上游已禁用：%s", detail)
		}
		if detail == "" {
			return "Upstream disabled"
		}
		return fmt.Sprintf("Upstream disabled: %s", detail)
	case "route_saved":
		if zh {
			if detail == "" {
				return "路由已保存"
			}
			return fmt.Sprintf("路由已保存：%s", detail)
		}
		if detail == "" {
			return "Route saved"
		}
		return fmt.Sprintf("Route saved: %s", detail)
	case "route_deleted":
		if zh {
			if detail == "" {
				return "路由已删除"
			}
			return fmt.Sprintf("路由已删除：%s", detail)
		}
		if detail == "" {
			return "Route deleted"
		}
		return fmt.Sprintf("Route deleted: %s", detail)
	case "plan_configs_updated":
		if zh {
			return "会员套餐配置已更新"
		}
		return "Membership plan configs updated"
	case "health_check_done":
		if zh {
			if detail == "" {
				return "上游诊断完成"
			}
			return fmt.Sprintf("上游诊断完成：%s", detail)
		}
		if detail == "" {
			return "Upstream diagnose completed"
		}
		return fmt.Sprintf("Upstream diagnose completed: %s", detail)
	default:
		_, localized := localizeMessage(r, code, raw)
		return localized
	}
}

func adminErrorMessage(r *http.Request, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	code, detail := splitAdminCodeDetail(normalizeAdminErrorCode(raw))
	zh := detectLanguage(r) == languageZhCN
	switch code {
	case "sign_in_required":
		if zh {
			return "请先登录"
		}
		return "Sign in required"
	case "invalid_session":
		if zh {
			return "会话无效，请重新登录"
		}
		return "Invalid session, please sign in again"
	case "user_disabled":
		if zh {
			return "用户已被禁用"
		}
		return "User disabled"
	case "invalid_form_data":
		if zh {
			return "表单数据不合法"
		}
		return "Invalid form data"
	case "api_key_required":
		if zh {
			return "缺少 api_key 参数"
		}
		return "api_key is required"
	case "key_hash_required":
		if zh {
			return "缺少 key_hash 参数"
		}
		return "key_hash is required"
	case "rpm_limit_invalid":
		if zh {
			return "rpm_limit 必须是正整数"
		}
		return "rpm_limit must be a positive integer"
	case "daily_token_limit_invalid":
		if zh {
			return "daily_token_limit 必须是正整数"
		}
		return "daily_token_limit must be a positive integer"
	case "amount_invalid":
		if zh {
			return "amount 必须是正数"
		}
		return "amount must be a positive number"
	case "balance_amount_invalid":
		if zh {
			return "余额必须大于或等于 0"
		}
		return "balance must be non-negative"
	case "missing_plan":
		if zh {
			return "缺少 plan_id 参数"
		}
		return "plan is required"
	case "missing_period_end":
		if zh {
			return "缺少 period_end 参数"
		}
		return "period_end is required"
	case "invalid_period_end":
		if zh {
			return "period_end 格式不合法"
		}
		return "invalid period_end"
	case "missing_user_id":
		if zh {
			return "缺少 user_id 参数"
		}
		return "missing user_id"
	case "password_confirmation_mismatch":
		if zh {
			return "两次密码输入不一致"
		}
		return "Password confirmation mismatch"
	case "save_api_key_failed":
		return adminPrefixedDetail(zh, "保存 API Key 失败", "Save API key failed", r, detail)
	case "delete_api_key_failed":
		return adminPrefixedDetail(zh, "删除 API Key 失败", "Delete API key failed", r, detail)
	case "save_prompt_api_failed":
		return adminPrefixedDetail(zh, "保存 Prompt API 失败", "Save Prompt API failed", r, detail)
	case "save_default_api_failed":
		return adminPrefixedDetail(zh, "保存默认 API 配置失败", "Save default API config failed", r, detail)
	case "topup_failed":
		return adminPrefixedDetail(zh, "充值失败", "Top up failed", r, detail)
	case "create_user_failed":
		return adminPrefixedDetail(zh, "创建用户失败", "Create user failed", r, detail)
	case "update_user_role_failed":
		return adminPrefixedDetail(zh, "更新用户角色失败", "Update user role failed", r, detail)
	case "update_user_status_failed":
		return adminPrefixedDetail(zh, "更新用户状态失败", "Update user status failed", r, detail)
	case "forbidden_self_sensitive_operation":
		if zh {
			return "管理员不能修改自己的角色或状态"
		}
		return "Admin cannot update their own role or status"
	case "forbidden_delete_self":
		if zh {
			return "管理员不能删除自己"
		}
		return "Admin cannot delete themselves"
	case "update_user_balance_failed":
		return adminPrefixedDetail(zh, "更新用户余额失败", "Update user balance failed", r, detail)
	case "update_user_membership_failed":
		return adminPrefixedDetail(zh, "更新用户会员失败", "Update user membership failed", r, detail)
	case "delete_user_failed":
		return adminPrefixedDetail(zh, "删除用户失败", "Delete user failed", r, detail)
	case "save_upstream_failed":
		return adminPrefixedDetail(zh, "保存上游失败", "Save upstream failed", r, detail)
	case "delete_upstream_failed":
		return adminPrefixedDetail(zh, "删除上游失败", "Delete upstream failed", r, detail)
	case "toggle_upstream_failed":
		return adminPrefixedDetail(zh, "切换上游状态失败", "Toggle upstream failed", r, detail)
	case "check_upstream_failed":
		return adminPrefixedDetail(zh, "上游诊断失败", "Upstream diagnose failed", r, detail)
	case "save_route_failed":
		return adminPrefixedDetail(zh, "保存路由失败", "Save route failed", r, detail)
	case "delete_route_failed":
		return adminPrefixedDetail(zh, "删除路由失败", "Delete route failed", r, detail)
	case "update_plan_configs_failed":
		return adminPrefixedDetail(zh, "更新会员套餐配置失败", "Update membership plan configs failed", r, detail)
	case "delete_prompt_api_failed":
		return adminPrefixedDetail(zh, "删除 Prompt API 失败", "Delete Prompt API failed", r, detail)
	case "delete_order_failed":
		return adminPrefixedDetail(zh, "删除订单失败", "Delete order failed", r, detail)
	case "missing_upstream_id":
		if zh {
			return "缺少 upstream id"
		}
		return "missing upstream id"
	case "missing_alias":
		if zh {
			return "缺少 alias"
		}
		return "missing alias"
	case "missing_slug":
		if zh {
			return "缺少 slug"
		}
		return "missing slug"
	case "missing_order_id":
		if zh {
			return "缺少 order_id"
		}
		return "missing order_id"
	case "user_exists_try_signin":
		if zh {
			return "用户已存在，请直接登录"
		}
		return "User already exists, try sign in"
	case "user_not_found_try_signup":
		if zh {
			return "用户不存在，请先注册"
		}
		return "User not found, try sign up"
	case "session_invalid_or_expired":
		if zh {
			return "会话无效或已过期"
		}
		return "Session is invalid or expired"
	case "permission_denied":
		if zh {
			return "权限不足"
		}
		return "Permission denied"
	case "owner_required":
		if zh {
			return "权限不足：需要 owner 角色"
		}
		return "Forbidden: owner required"
	default:
		_, localized := localizeMessage(r, code, raw)
		return localized
	}
}

func adminIssueMessage(r *http.Request, key string, err error) string {
	if err == nil {
		return ""
	}
	zh := detectLanguage(r) == languageZhCN
	detail := adminDetailMessage(r, err.Error())

	var prefix string
	switch key {
	case "list_users":
		if zh {
			prefix = "加载用户列表失败"
		} else {
			prefix = "List users failed"
		}
	case "list_api_keys":
		if zh {
			prefix = "加载 API Key 列表失败"
		} else {
			prefix = "List API keys failed"
		}
	case "list_prompt_apis":
		if zh {
			prefix = "加载 Prompt API 列表失败"
		} else {
			prefix = "List Prompt APIs failed"
		}
	case "load_balance":
		if zh {
			prefix = "加载余额失败"
		} else {
			prefix = "Load balance failed"
		}
	case "list_orders":
		if zh {
			prefix = "加载订单列表失败"
		} else {
			prefix = "List orders failed"
		}
	case "list_audits":
		if zh {
			prefix = "加载审计日志失败"
		} else {
			prefix = "List audits failed"
		}
	default:
		if zh {
			prefix = "加载数据失败"
		} else {
			prefix = "Load data failed"
		}
	}
	return prefix + ": " + detail
}

func adminPrefixedDetail(zh bool, zhPrefix, enPrefix string, r *http.Request, detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		if zh {
			return zhPrefix
		}
		return enPrefix
	}
	if zh {
		return zhPrefix + "：" + adminDetailMessage(r, detail)
	}
	return enPrefix + ": " + adminDetailMessage(r, detail)
}

func adminDetailMessage(r *http.Request, detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return detail
	}

	code, _ := splitAdminCodeDetail(detail)
	if code != detail || isKnownAdminDetailCode(code) {
		return adminErrorMessage(r, detail)
	}

	_, localized := localizeMessage(r, "", detail)
	return localized
}

func splitAdminCodeDetail(raw string) (code, detail string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	parts := strings.SplitN(raw, ":", 2)
	code = strings.TrimSpace(parts[0])
	if len(parts) == 2 {
		detail = strings.TrimSpace(parts[1])
	}
	return code, detail
}

func normalizeAdminErrorCode(raw string) string {
	raw = strings.TrimSpace(raw)
	switch raw {
	case "policy sync unavailable":
		return "policy_sync_unavailable"
	case "prompt api service unavailable":
		return "prompt_api_unavailable"
	case "billing service unavailable":
		return "billing_unavailable"
	case "user service unavailable":
		return "user_service_unavailable"
	case "invalid form data":
		return "invalid_form_data"
	case "api_key is required":
		return "api_key_required"
	case "key_hash is required":
		return "key_hash_required"
	case "rpm_limit must be a positive integer":
		return "rpm_limit_invalid"
	case "daily_token_limit must be a positive integer":
		return "daily_token_limit_invalid"
	case "amount must be a positive number":
		return "amount_invalid"
	case "balance must be non-negative":
		return "balance_amount_invalid"
	case "plan is required":
		return "missing_plan"
	case "period_end is required":
		return "missing_period_end"
	case "invalid period_end":
		return "invalid_period_end"
	case "invalid period end":
		return "invalid_period_end"
	case "user already exists, try sign in":
		return "user_exists_try_signin"
	case "user not found, try sign up":
		return "user_not_found_try_signup"
	case "session is invalid or expired":
		return "session_invalid_or_expired"
	case "permission denied":
		return "permission_denied"
	case "forbidden: owner required":
		return "owner_required"
	case "missing user_id":
		return "missing_user_id"
	case "password confirmation mismatch":
		return "password_confirmation_mismatch"
	case "forbidden self sensitive operation":
		return "forbidden_self_sensitive_operation"
	case "forbidden delete self":
		return "forbidden_delete_self"
	case "missing upstream id":
		return "missing_upstream_id"
	case "missing alias":
		return "missing_alias"
	case "missing slug":
		return "missing_slug"
	case "missing order_id":
		return "missing_order_id"
	default:
		return raw
	}
}

func isKnownAdminDetailCode(code string) bool {
	switch code {
	case "user_exists_try_signin", "user_not_found_try_signup", "session_invalid_or_expired", "permission_denied", "owner_required":
		return true
	default:
		return false
	}
}

func adminUI(lang, key string) string {
	zh := isChineseLang(lang)
	switch key {
	case "login_title":
		if zh {
			return "opcAPI 管理登录"
		}
		return "opcAPI Admin Login"
	case "login_heading":
		if zh {
			return "opcAPI 管理后台"
		}
		return "opcAPI Admin"
	case "login_subtitle":
		if zh {
			return "服务端渲染管理控制台。可登录或创建租户所有者账号。"
		}
		return "Server-rendered management console. Sign in or create a tenant owner account."
	case "management":
		if zh {
			return "管理后台"
		}
		return "MANAGEMENT"
	case "users_and_keys":
		if zh {
			return "用户与密钥"
		}
		return "Users & Keys"
	case "order_management":
		if zh {
			return "订单管理"
		}
		return "Order Management"
	case "upstreams_and_routing":
		if zh {
			return "上游与路由"
		}
		return "Upstreams & Routing"
	case "admin_signin_subtitle":
		if zh {
			return "使用管理员账户登录管理控制台。"
		}
		return "Use your administrator account to enter the management console."
	case "signin_tip":
		if zh {
			return "如果是首次使用，请先完成管理员初始化。"
		}
		return "If this is your first time, initialize admin from setup flow."
	case "bootstrap":
		if zh {
			return "系统初始化"
		}
		return "BOOTSTRAP"
	case "initialize_admin":
		if zh {
			return "初始化管理员"
		}
		return "Initialize Admin"
	case "admin_setup_subtitle":
		if zh {
			return "创建首个管理员账户。仅在系统尚未存在管理员时出现。"
		}
		return "Create the first admin account. This page appears only when no admin exists."
	case "admin_setup_tip":
		if zh {
			return "请使用生产环境强密码并妥善保管。"
		}
		return "Use a production-grade password and store it securely."
	case "create_super_admin":
		if zh {
			return "创建超级管理员"
		}
		return "Create Super Admin"
	case "email":
		if zh {
			return "邮箱"
		}
		return "Email"
	case "password":
		if zh {
			return "密码"
		}
		return "Password"
	case "confirm_password":
		if zh {
			return "确认密码"
		}
		return "Confirm Password"
	case "user_operations":
		if zh {
			return "用户操作"
		}
		return "User Operations"
	case "update_role":
		if zh {
			return "更新角色"
		}
		return "Update Role"
	case "update_status":
		if zh {
			return "更新状态"
		}
		return "Update Status"
	case "create_update_key_policy":
		if zh {
			return "创建 / 更新 Key 策略"
		}
		return "Create / Update Key Policy"
	case "api_key_policies":
		if zh {
			return "API Key 策略"
		}
		return "API Key Policies"
	case "delete_key_policy":
		if zh {
			return "删除 Key 策略"
		}
		return "Delete Key Policy"
	case "billing_topup":
		if zh {
			return "充值管理"
		}
		return "Billing Topup"
	case "upstream_providers":
		if zh {
			return "上游服务"
		}
		return "Upstream Providers"
	case "upstream_operations":
		if zh {
			return "上游操作"
		}
		return "Upstream Operations"
	case "create_update_upstream":
		if zh {
			return "创建 / 更新上游"
		}
		return "Create / Update Upstream"
	case "delete_upstream":
		if zh {
			return "删除上游"
		}
		return "Delete Upstream"
	case "toggle_upstream":
		if zh {
			return "切换上游状态"
		}
		return "Toggle Upstream"
	case "bulk_health_check":
		if zh {
			return "批量诊断"
		}
		return "Bulk Diagnose"
	case "check_all_upstreams":
		if zh {
			return "诊断全部上游"
		}
		return "Diagnose All Upstreams"
	case "check":
		if zh {
			return "诊断"
		}
		return "Diagnose"
	case "model_routes":
		if zh {
			return "模型路由"
		}
		return "Model Routes"
	case "create_update_route":
		if zh {
			return "创建 / 更新路由"
		}
		return "Create / Update Route"
	case "delete_route":
		if zh {
			return "删除路由"
		}
		return "Delete Route"
	case "routing_runtime_stats":
		if zh {
			return "路由运行统计"
		}
		return "Routing Runtime Stats"
	case "health":
		if zh {
			return "连通性"
		}
		return "Reachability"
	case "latency":
		if zh {
			return "延迟"
		}
		return "Latency"
	case "checked_at":
		if zh {
			return "诊断时间"
		}
		return "Diagnosed At"
	case "base_url":
		if zh {
			return "基础 URL"
		}
		return "Base URL"
	case "targets":
		if zh {
			return "目标"
		}
		return "Targets"
	case "upstream_id":
		if zh {
			return "上游 ID"
		}
		return "Upstream ID"
	case "model_alias":
		if zh {
			return "模型别名"
		}
		return "Model Alias"
	case "strategy":
		if zh {
			return "策略"
		}
		return "Strategy"
	case "last_error":
		if zh {
			return "最近错误"
		}
		return "Last Error"
	case "last_seen":
		if zh {
			return "最近出现"
		}
		return "Last Seen"
	case "save_upstream":
		if zh {
			return "保存上游"
		}
		return "Save Upstream"
	case "save_route":
		if zh {
			return "保存路由"
		}
		return "Save Route"
	case "submitting":
		if zh {
			return "提交中..."
		}
		return "Submitting..."
	case "saving":
		if zh {
			return "保存中..."
		}
		return "Saving..."
	case "deleting":
		if zh {
			return "删除中..."
		}
		return "Deleting..."
	case "checking":
		if zh {
			return "诊断中..."
		}
		return "Diagnosing..."
	case "delete_this_key_policy":
		if zh {
			return "确认删除这条 Key 策略？"
		}
		return "Delete this key policy?"
	case "delete_this_upstream":
		if zh {
			return "确认删除这个上游？"
		}
		return "Delete this upstream?"
	case "delete_this_route":
		if zh {
			return "确认删除这条路由？"
		}
		return "Delete this route?"
	case "amount_positive_hint":
		if zh {
			return "金额必须大于 0"
		}
		return "Amount must be positive"
	case "at_least_one_route_target":
		if zh {
			return "请至少填写一个路由目标。"
		}
		return "Please provide at least one route target."
	case "at_least_one_positive_value":
		if zh {
			return "请输入大于 0 的数值"
		}
		return "Please input a value greater than 0"
	case "fill_selected_users":
		if zh {
			return "填入已选用户"
		}
		return "Fill Selected Users"
	case "no_users_selected":
		if zh {
			return "请先选择至少一个用户"
		}
		return "Please select at least one user"
	case "channel":
		if zh {
			return "渠道"
		}
		return "Channel"
	case "identifier":
		if zh {
			return "标识"
		}
		return "Identifier"
	case "verification_code_phone":
		if zh {
			return "验证码（仅手机）"
		}
		return "Verification code (for phone)"
	case "tenant_name_signup":
		if zh {
			return "租户名（仅注册时需要）"
		}
		return "Tenant name (only for sign up)"
	case "sign_in":
		if zh {
			return "登录"
		}
		return "Sign In"
	case "sign_up":
		if zh {
			return "注册"
		}
		return "Sign Up"
	case "console_title":
		if zh {
			return "opcAPI 管理控制台"
		}
		return "opcAPI Admin Console"
	case "sign_out":
		if zh {
			return "退出登录"
		}
		return "Sign Out"
	case "partial_data_errors":
		if zh {
			return "部分数据加载失败："
		}
		return "Partial data errors:"
	case "tenant":
		if zh {
			return "租户"
		}
		return "Tenant"
	case "user":
		if zh {
			return "用户"
		}
		return "User"
	case "current_user":
		if zh {
			return "当前用户"
		}
		return "Current User"
	case "balance":
		if zh {
			return "余额"
		}
		return "Balance"
	case "updated":
		if zh {
			return "更新时间"
		}
		return "updated"
	case "quick_stats":
		if zh {
			return "快速统计"
		}
		return "Quick Stats"
	case "api_keys":
		if zh {
			return "API 密钥"
		}
		return "API Keys"
	case "prompt_apis":
		if zh {
			return "APIs"
		}
		return "APIs"
	case "users":
		if zh {
			return "用户"
		}
		return "Users"
	case "recent_orders":
		if zh {
			return "最近订单"
		}
		return "Recent Orders"
	case "audit_logs":
		if zh {
			return "审计日志"
		}
		return "Audit Logs"
	case "created":
		if zh {
			return "创建时间"
		}
		return "Created"
	case "name":
		if zh {
			return "名称"
		}
		return "Name"
	case "status":
		if zh {
			return "状态"
		}
		return "Status"
	case "amount":
		if zh {
			return "金额"
		}
		return "Amount"
	case "currency":
		if zh {
			return "币种"
		}
		return "Currency"
	case "description":
		if zh {
			return "描述"
		}
		return "Description"
	case "top_up":
		if zh {
			return "充值"
		}
		return "Top Up"
	case "api_key":
		if zh {
			return "API 密钥"
		}
		return "API Key"
	case "rpm_limit":
		if zh {
			return "RPM 限制"
		}
		return "RPM Limit"
	case "rpm":
		if zh {
			return "RPM"
		}
		return "RPM"
	case "daily":
		if zh {
			return "每日"
		}
		return "Daily"
	case "save":
		if zh {
			return "保存"
		}
		return "Save"
	case "delete":
		if zh {
			return "删除"
		}
		return "Delete"
	case "save_api_key_policy":
		if zh {
			return "保存 API Key 策略"
		}
		return "Save API Key Policy"
	case "slug":
		if zh {
			return "标识"
		}
		return "Slug"
	case "model":
		if zh {
			return "模型"
		}
		return "Model"
	case "system_prompt":
		if zh {
			return "系统提示词"
		}
		return "System Prompt"
	case "user_template":
		if zh {
			return "用户模板"
		}
		return "User Template"
	case "variables_comma":
		if zh {
			return "变量（逗号分隔）"
		}
		return "Variables (comma separated)"
	case "publish":
		if zh {
			return "发布"
		}
		return "publish"
	case "save_prompt_api":
		if zh {
			return "保存 Prompt API"
		}
		return "Save Prompt API"
	case "role":
		if zh {
			return "角色"
		}
		return "Role"
	case "verification_code_phone_only":
		if zh {
			return "验证码（仅手机）"
		}
		return "Verification code (phone only)"
	case "create_user":
		if zh {
			return "创建用户"
		}
		return "Create User"
	case "id":
		if zh {
			return "ID"
		}
		return "ID"
	case "plan":
		if zh {
			return "套餐"
		}
		return "Plan"
	case "phone":
		if zh {
			return "手机号"
		}
		return "Phone"
	case "wechat":
		if zh {
			return "微信"
		}
		return "WeChat"
	case "feishu":
		if zh {
			return "飞书"
		}
		return "Feishu"
	case "key_hash":
		if zh {
			return "密钥哈希"
		}
		return "Key Hash"
	case "enabled":
		if zh {
			return "启用"
		}
		return "Enabled"
	case "daily_token_limit":
		if zh {
			return "每日 Token 上限"
		}
		return "Daily Token Limit"
	case "version":
		if zh {
			return "版本"
		}
		return "Version"
	case "variables":
		if zh {
			return "变量"
		}
		return "Variables"
	case "user_id":
		if zh {
			return "用户 ID"
		}
		return "User ID"
	case "type":
		if zh {
			return "类型"
		}
		return "Type"
	case "action":
		if zh {
			return "操作"
		}
		return "Action"
	case "actor":
		if zh {
			return "操作人"
		}
		return "Actor"
	case "target":
		if zh {
			return "目标"
		}
		return "Target"
	case "metadata":
		if zh {
			return "元数据"
		}
		return "Metadata"
	case "resource":
		if zh {
			return "资源"
		}
		return "Resource"
	case "placeholder_phone_openid":
		if zh {
			return "手机号/openid"
		}
		return "phone/openid"
	case "placeholder_manual_topup":
		if zh {
			return "手动充值"
		}
		return "manual topup"
	case "placeholder_email_summary":
		if zh {
			return "邮件摘要"
		}
		return "email_summary"
	case "placeholder_email_summary_name":
		if zh {
			return "邮件摘要"
		}
		return "Email Summary"
	case "default_user_template":
		if zh {
			return "总结：{{.content}}"
		}
		return "Summarize: {{.content}}"
	default:
		return key
	}
}

func adminEnum(lang, kind, raw string) string {
	zh := isChineseLang(lang)
	v := strings.TrimSpace(strings.ToLower(raw))
	switch kind {
	case "channel":
		switch v {
		case "phone":
			return adminUI(lang, "phone")
		case "wechat":
			return adminUI(lang, "wechat")
		case "feishu":
			return adminUI(lang, "feishu")
		case "manual":
			if zh {
				return "手动"
			}
			return "manual"
		}
	case "role":
		switch v {
		case "owner":
			if zh {
				return "所有者"
			}
			return "owner"
		case "admin":
			if zh {
				return "管理员"
			}
			return "admin"
		case "member":
			if zh {
				return "成员"
			}
			return "member"
		}
	case "status":
		switch v {
		case "active":
			if zh {
				return "启用"
			}
			return "active"
		case "disabled":
			if zh {
				return "禁用"
			}
			return "disabled"
		case "published":
			if zh {
				return "已发布"
			}
			return "published"
		case "draft":
			if zh {
				return "草稿"
			}
			return "draft"
		case "enabled":
			if zh {
				return "启用"
			}
			return "enabled"
		case "healthy":
			if zh {
				return "可访问"
			}
			return "reachable"
		case "unhealthy":
			if zh {
				return "不可访问"
			}
			return "unreachable"
		}
	case "order_type":
		switch v {
		case "topup":
			if zh {
				return "充值"
			}
			return "topup"
		case "subscription":
			if zh {
				return "订阅"
			}
			return "subscription"
		case "usage":
			if zh {
				return "用量扣费"
			}
			return "usage"
		}
	case "order_status":
		switch v {
		case "pending":
			if zh {
				return "处理中"
			}
			return "pending"
		case "paid", "succeeded", "success":
			if zh {
				return "成功"
			}
			return raw
		case "failed":
			if zh {
				return "失败"
			}
			return "failed"
		case "canceled", "cancelled":
			if zh {
				return "已取消"
			}
			return raw
		}
	case "audit_resource":
		switch v {
		case "api_key":
			if zh {
				return "API 密钥"
			}
			return "api_key"
		case "user":
			if zh {
				return "用户"
			}
			return "user"
		case "prompt_api":
			if zh {
				return "Prompt API"
			}
			return "prompt_api"
		case "billing":
			if zh {
				return "计费"
			}
			return "billing"
		}
	case "audit_action":
		switch v {
		case "create":
			if zh {
				return "创建"
			}
			return "create"
		case "delete":
			if zh {
				return "删除"
			}
			return "delete"
		case "upsert":
			if zh {
				return "创建/更新"
			}
			return "upsert"
		case "update_role":
			if zh {
				return "更新角色"
			}
			return "update_role"
		case "update_status":
			if zh {
				return "更新状态"
			}
			return "update_status"
		}
	}
	return raw
}
