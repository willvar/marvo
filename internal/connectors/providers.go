package connectors

const (
	categoryUniversal = "通用"
	categoryChat      = "协作与通讯"
	categoryPush      = "推送服务"
	categoryIncident  = "事件响应"
	categorySMS       = "短信与语音"
	categoryEmail     = "邮件"
	categoryRegional  = "区域服务"
	categoryOther     = "其它"
)

func registerProviders(registry *Registry) {
	registerUniversalProviders(registry)
	registerChatProviders(registry)
	registerPushProviders(registry)
	registerIncidentProviders(registry)
	registerSMSProviders(registry)
	registerEmailProviders(registry)
	registerRegionalProviders(registry)
	registerOtherProviders(registry)
}

func provider(id, name, category, documentation string, fields []Field, send SendFunc) Provider {
	return Provider{ID: id, Name: name, Category: category, Documentation: documentation, Fields: fields, send: send}
}
