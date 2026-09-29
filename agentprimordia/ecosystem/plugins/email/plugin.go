package email

import (
	"context"
	"encoding/json"
	"fmt"
	"net/smtp"
	"strconv"
	"strings"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

// Plugin 是邮件发送插件，提供 SMTP 邮件发送能力
type Plugin struct {
	tool *EmailTool
}

// New 创建新的 Email 插件实例
func New() *Plugin {
	return &Plugin{tool: &EmailTool{}}
}

// Name 返回插件名称
func (p *Plugin) Name() string { return "email" }

// Version 返回插件版本
func (p *Plugin) Version() string { return "0.1.0" }

// Tools 返回插件提供的工具列表
func (p *Plugin) Tools() []ap.Tool {
	return []ap.Tool{p.tool}
}

// Init 初始化插件，从 config 中读取 SMTP 配置
func (p *Plugin) Init(config map[string]any) error {
	if config == nil {
		return fmt.Errorf("email 插件需要 SMTP 配置")
	}

	host, _ := config["smtp_host"].(string)
	if host == "" {
		return fmt.Errorf("缺少 smtp_host 配置")
	}

	port := 587
	if p, ok := config["smtp_port"]; ok {
		switch v := p.(type) {
		case float64:
			port = int(v)
		case string:
			if n, err := strconv.Atoi(v); err == nil {
				port = n
			}
		}
	}

	username, _ := config["smtp_username"].(string)
	password, _ := config["smtp_password"].(string)
	fromAddr, _ := config["from_address"].(string)

	if username == "" || password == "" {
		return fmt.Errorf("缺少 smtp_username 或 smtp_password 配置")
	}

	p.tool.host = host
	p.tool.port = port
	p.tool.username = username
	p.tool.password = password
	p.tool.fromAddress = fromAddr
	if p.tool.fromAddress == "" {
		p.tool.fromAddress = username
	}

	return nil
}

// Close 关闭插件资源
func (p *Plugin) Close() error { return nil }

// EmailTool 是基于 SMTP 的邮件发送工具
type EmailTool struct {
	host        string
	port        int
	username    string
	password    string
	fromAddress string
}

// Name 返回工具名称
func (t *EmailTool) Name() string { return "email_sender" }

// Description 返回工具描述
func (t *EmailTool) Description() string {
	return `邮件发送工具，通过 SMTP 协议发送邮件。
功能：
- 发送纯文本和 HTML 格式邮件
- 支持抄送（CC）和密送（BCC）
- SMTP 认证登录

参数：
- to (required): 收件人地址，多个用逗号分隔
- subject (required): 邮件主题
- body (required): 邮件正文
- cc (optional): 抄送地址，多个用逗号分隔
- bcc (optional): 密送地址，多个用逗号分隔
- content_type (optional): 内容类型 [text|html]，默认 text`
}

// Parameters 返回工具参数的 JSON Schema
func (t *EmailTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"to": {"type": "string", "description": "收件人地址，多个用逗号分隔"},
			"subject": {"type": "string", "description": "邮件主题"},
			"body": {"type": "string", "description": "邮件正文"},
			"cc": {"type": "string", "description": "抄送地址，多个用逗号分隔"},
			"bcc": {"type": "string", "description": "密送地址，多个用逗号分隔"},
			"content_type": {"type": "string", "enum": ["text", "html"], "description": "内容类型，默认 text"}
		},
		"required": ["to", "subject", "body"]
	}`)
}

// Category 返回工具分类
func (t *EmailTool) Category() string { return "communication" }

// Execute 执行邮件发送
func (t *EmailTool) Execute(ctx context.Context, input json.RawMessage) (*ap.ToolResult, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("解析参数错误: %w", err)
	}

	to, _ := params["to"].(string)
	if to == "" {
		return ap.NewToolErrorResult("参数 'to' 不能为空"), nil
	}
	subject, _ := params["subject"].(string)
	if subject == "" {
		return ap.NewToolErrorResult("参数 'subject' 不能为空"), nil
	}
	body, _ := params["body"].(string)
	if body == "" {
		return ap.NewToolErrorResult("参数 'body' 不能为空"), nil
	}

	contentType := "text/plain"
	if ct, ok := params["content_type"].(string); ok && ct == "html" {
		contentType = "text/html"
	}

	ccStr, _ := params["cc"].(string)
	bccStr, _ := params["bcc"].(string)

	// 构建收件人列表（用于 RCPT TO，包含 to + cc + bcc）
	toList := splitAddresses(to)
	ccList := splitAddresses(ccStr)
	bccList := splitAddresses(bccStr)

	allRecipients := make([]string, 0, len(toList)+len(ccList)+len(bccList))
	allRecipients = append(allRecipients, toList...)
	allRecipients = append(allRecipients, ccList...)
	allRecipients = append(allRecipients, bccList...)

	// 构建邮件内容
	var msg strings.Builder
	msg.WriteString("From: " + t.fromAddress + "\r\n")
	msg.WriteString("To: " + to + "\r\n")
	if ccStr != "" {
		msg.WriteString("Cc: " + ccStr + "\r\n")
	}
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("Content-Type: " + contentType + "; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	// 发送邮件
	addr := t.host + ":" + strconv.Itoa(t.port)
	auth := smtp.PlainAuth("", t.username, t.password, t.host)

	err := smtp.SendMail(addr, auth, t.fromAddress, allRecipients, []byte(msg.String()))
	if err != nil {
		return ap.NewToolErrorResult(fmt.Sprintf("邮件发送失败: %v", err)), nil
	}

	result := map[string]any{
		"success":      true,
		"from":         t.fromAddress,
		"to":           toList,
		"cc":           ccList,
		"bcc":          bccList,
		"subject":      subject,
		"content_type": contentType,
	}
	output, _ := json.MarshalIndent(result, "", "  ")
	return &ap.ToolResult{Content: string(output)}, nil
}

// splitAddresses 将逗号分隔的地址字符串拆分为地址列表
func splitAddresses(addrStr string) []string {
	if addrStr == "" {
		return nil
	}
	parts := strings.Split(addrStr, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
