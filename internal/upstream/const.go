// 常量：CodeArts Agent 云端 API（来自 resources/app/extensions/vscode-codebot 逆向）。
package upstream

const (
	// SnapEngineApiHost 盘古引擎/聊天网关（snap-access，product.json snapEngineDomain）。
	SnapEngineApiHost = "https://snap-access.cn-north-4.myhuaweicloud.com"
	// SnapManagerHost 登录/令牌服务（snap-manager）。
	SnapManagerHost = SnapEngineApiHost + "/snap-manager"
	// PortalHost 华为云 CodeArts 门户（OAuth authorize 入口，注意带 /portal）。
	PortalHost = "https://codearts.huaweicloud.com/portal"
	// IAMHost IAM 令牌/STS（备用）。
	IAMHost = "https://iam.myhuaweicloud.com"
	// STSHost 临时 AK/SK。
	STSHost = "https://sts.cn-north-4.myhuaweicloud.com"

	// CLIENT_ID OAuth client（官方 CodeArts Agent 插件的 uri_scheme）。
	// refresh_token 与该值绑定：换 client_id 刷新会被 STS 拒
	// （invalid refresh token: 'invalid client id: xxx'）。实测 2026-09-15。
	CLIENT_ID = "codearts-agent"

	// 聊天端点（chatDomain 商业版 = snap-access.cn-north-4）。
	EpChatV2         = "/api/v2/chat/completions"
	EpChat           = "/v1/chat/chat"
	EpStartChat      = "/v1/chat/start-chat"
	EpChatAgents     = "/v1/chat/agents"
	EpChatManagement = "/v1/chat/management/event"
	EpChatRecord     = "/v1/chat/record-request"
	EpAgentList      = "/v1/agent-center/agents/useragents"
	EpAgentDetail    = "/v1/agent-center/agents/detail"
	// 内置模型（与官方 ModelService.listBuiltinModels 一致，Agent-Type: PromptCenter）。
	EpModelBuiltin = "/v1/model/builtin"

	// 限时福利（免费套餐）网关：模型发现 + 领取（opengw.developer 域）。
	BenefitHost     = "https://opengw.developer.huaweicloud.com"
	EpBenefitConfig = "/api/v1/gateway/config"
	EpBenefitClaim  = "/api/v1/benefit/claim"
	// 福利网关总开关。注意它挂在 snap-access 域，不 opengw 域
	// （opengw 上该路径是 404 APIG.0101）。返回裸对象 {"enabled": bool}，
	// 没有 {code,data} 信封。未开通时 opengw 的 claim/config/balance 一律回
	// 4000 invalid channel, group not found。
	EpBenefitGatewayConfig = "/v1/benefit-gateway-config"

	// MaxOutputTokens 上游 InferHub 接受的 max_tokens 硬上限（实测 65536 通过、
	// 66560 起整请求被拒，报流内错误帧 InferHub.001001005.400 "The request param
	// is invalid"）。AgentCenter/opengw 下发的 model_parameters.max_tokens 是上下文
	// 口径的值（131072 起），客户端读到后照抄发送必然全量失败。
	MaxOutputTokens = 65536

	// 登录/令牌
	EpLoginTicket    = "/v1/login/ticket"
	EpOAuthTokens    = "/v1/oauth2/tokens"
	EpCurrentUser    = "/v1/current/user"
	EpCallerIdentity = "/v5/caller-identity"
)
