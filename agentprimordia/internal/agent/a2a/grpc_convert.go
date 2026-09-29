package a2a

import (
	"time"

	a2av1 "github.com/Gleamseekers/AgentPrimordia/internal/agent/a2a/proto/a2a/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// toProtoTimestamp 将 time.Time 转换为 protobuf Timestamp。
func toProtoTimestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// fromProtoTimestamp 将 protobuf Timestamp 转换为 time.Time。
func fromProtoTimestamp(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

// toProtoAgentCard 将 AgentCard 转换为 proto。
func toProtoAgentCard(card *AgentCard) *a2av1.AgentCard {
	if card == nil {
		return nil
	}
	protoCard := &a2av1.AgentCard{
		Protocol:     card.Protocol,
		AgentId:      card.AgentID,
		Name:         card.Name,
		Description:  card.Description,
		Capabilities: toProtoAgentCapabilities(&card.Capabilities),
		Endpoints:    toProtoAgentEndpoints(&card.Endpoints),
		Metadata:     card.Metadata,
	}
	for _, s := range card.SecuritySchemes {
		protoCard.SecuritySchemes = append(protoCard.SecuritySchemes, toProtoSecurityScheme(s))
	}
	for _, s := range card.Skills {
		protoCard.Skills = append(protoCard.Skills, toProtoAgentSkill(s))
	}
	return protoCard
}

// fromProtoAgentCard 将 proto 转换为 AgentCard。
func fromProtoAgentCard(card *a2av1.AgentCard) *AgentCard {
	if card == nil {
		return nil
	}
	ac := &AgentCard{
		Protocol:     card.Protocol,
		AgentID:      card.AgentId,
		Name:         card.Name,
		Description:  card.Description,
		Capabilities: *fromProtoAgentCapabilities(card.Capabilities),
		Endpoints:    *fromProtoAgentEndpoints(card.Endpoints),
		Metadata:     card.Metadata,
	}
	for _, s := range card.SecuritySchemes {
		ac.SecuritySchemes = append(ac.SecuritySchemes, fromProtoSecurityScheme(s))
	}
	for _, s := range card.Skills {
		ac.Skills = append(ac.Skills, fromProtoAgentSkill(s))
	}
	return ac
}

func toProtoAgentCapabilities(c *AgentCapabilities) *a2av1.AgentCapabilities {
	if c == nil {
		return nil
	}
	return &a2av1.AgentCapabilities{
		InputModes:  c.InputModes,
		OutputModes: c.OutputModes,
		Streaming:   c.Streaming,
	}
}

func fromProtoAgentCapabilities(c *a2av1.AgentCapabilities) *AgentCapabilities {
	if c == nil {
		return &AgentCapabilities{}
	}
	return &AgentCapabilities{
		InputModes:  c.InputModes,
		OutputModes: c.OutputModes,
		Streaming:   c.Streaming,
	}
}

func toProtoAgentEndpoints(e *AgentEndpoints) *a2av1.AgentEndpoints {
	if e == nil {
		return nil
	}
	return &a2av1.AgentEndpoints{
		BaseUrl:       e.BaseURL,
		TaskSend:      e.TaskSend,
		TaskGet:       e.TaskGet,
		TaskCancel:    e.TaskCancel,
		TaskSubscribe: e.TaskSubscribe,
		AgentCardUrl:  e.AgentCardURL,
	}
}

func fromProtoAgentEndpoints(e *a2av1.AgentEndpoints) *AgentEndpoints {
	if e == nil {
		return &AgentEndpoints{}
	}
	return &AgentEndpoints{
		BaseURL:       e.BaseUrl,
		TaskSend:      e.TaskSend,
		TaskGet:       e.TaskGet,
		TaskCancel:    e.TaskCancel,
		TaskSubscribe: e.TaskSubscribe,
		AgentCardURL:  e.AgentCardUrl,
	}
}

func toProtoSecurityScheme(s SecurityScheme) *a2av1.SecurityScheme {
	return &a2av1.SecurityScheme{
		Scheme: string(s.Scheme),
		In:     s.In,
		Name:   s.Name,
		Scopes: s.Scopes,
	}
}

func fromProtoSecurityScheme(s *a2av1.SecurityScheme) SecurityScheme {
	if s == nil {
		return SecurityScheme{}
	}
	return SecurityScheme{
		Scheme: AuthType(s.Scheme),
		In:     s.In,
		Name:   s.Name,
		Scopes: s.Scopes,
	}
}

func toProtoAgentSkill(s AgentSkill) *a2av1.AgentSkill {
	return &a2av1.AgentSkill{
		Id:          s.ID,
		Name:        s.Name,
		Description: s.Description,
		InputModes:  s.InputModes,
		OutputModes: s.OutputModes,
	}
}

func fromProtoAgentSkill(s *a2av1.AgentSkill) AgentSkill {
	if s == nil {
		return AgentSkill{}
	}
	return AgentSkill{
		ID:          s.Id,
		Name:        s.Name,
		Description: s.Description,
		InputModes:  s.InputModes,
		OutputModes: s.OutputModes,
	}
}

// toProtoTask 将 Task 转换为 proto。
func toProtoTask(task *Task) *a2av1.Task {
	if task == nil {
		return nil
	}
	protoTask := &a2av1.Task{
		Id:        task.ID,
		SessionId: task.SessionID,
		State:     string(task.State),
		Message:   toProtoMessage(task.Message),
		Status:    toProtoTaskStatus(task.Status),
		CreatedAt: toProtoTimestamp(task.CreatedAt),
		UpdatedAt: toProtoTimestamp(task.UpdatedAt),
		ExpiresAt: toProtoTimestamp(task.ExpiresAt),
	}
	for _, a := range task.Artifacts {
		protoTask.Artifacts = append(protoTask.Artifacts, toProtoArtifact(a))
	}
	return protoTask
}

// fromProtoTask 将 proto 转换为 Task。
func fromProtoTask(task *a2av1.Task) *Task {
	if task == nil {
		return nil
	}
	t := &Task{
		ID:        task.Id,
		SessionID: task.SessionId,
		State:     TaskState(task.State),
		Message:   fromProtoMessage(task.Message),
		Status:    fromProtoTaskStatus(task.Status),
		CreatedAt: fromProtoTimestamp(task.CreatedAt),
		UpdatedAt: fromProtoTimestamp(task.UpdatedAt),
		ExpiresAt: fromProtoTimestamp(task.ExpiresAt),
	}
	for _, a := range task.Artifacts {
		t.Artifacts = append(t.Artifacts, fromProtoArtifact(a))
	}
	return t
}

func toProtoTaskStatus(s *TaskStatus) *a2av1.TaskStatus {
	if s == nil {
		return nil
	}
	return &a2av1.TaskStatus{
		State:         string(s.State),
		ErrorMessage:  s.ErrorMessage,
		StreamMessage: toProtoMessage(s.StreamMessage),
	}
}

func fromProtoTaskStatus(s *a2av1.TaskStatus) *TaskStatus {
	if s == nil {
		return nil
	}
	return &TaskStatus{
		State:         TaskState(s.State),
		ErrorMessage:  s.ErrorMessage,
		StreamMessage: fromProtoMessage(s.StreamMessage),
	}
}

// toProtoMessage 将 A2AMessage 转换为 proto。
func toProtoMessage(msg *A2AMessage) *a2av1.Message {
	if msg == nil {
		return nil
	}
	protoMsg := &a2av1.Message{
		Role:      msg.Role,
		MessageId: msg.MessageID,
		ParentId:  msg.ParentID,
	}
	for _, p := range msg.Parts {
		protoMsg.Parts = append(protoMsg.Parts, toProtoPart(p))
	}
	return protoMsg
}

// fromProtoMessage 将 proto 转换为 A2AMessage。
func fromProtoMessage(msg *a2av1.Message) *A2AMessage {
	if msg == nil {
		return nil
	}
	m := &A2AMessage{
		Role:      msg.Role,
		MessageID: msg.MessageId,
		ParentID:  msg.ParentId,
	}
	for _, p := range msg.Parts {
		m.Parts = append(m.Parts, fromProtoPart(p))
	}
	return m
}

// toProtoPart 将 Part 接口转换为 proto。
func toProtoPart(part Part) *a2av1.Part {
	if part == nil {
		return nil
	}
	p := &a2av1.Part{Type: part.Type()}
	switch v := part.(type) {
	case TextPart:
		p.Content = &a2av1.Part_Text{Text: &a2av1.TextPart{Text: v.Text}}
	case *TextPart:
		if v != nil {
			p.Content = &a2av1.Part_Text{Text: &a2av1.TextPart{Text: v.Text}}
		}
	case FilePart:
		p.Content = &a2av1.Part_File{File: toProtoFilePart(v)}
	case *FilePart:
		if v != nil {
			p.Content = &a2av1.Part_File{File: toProtoFilePart(*v)}
		}
	case DataPart:
		p.Content = &a2av1.Part_Data{Data: &a2av1.DataPart{Data: v.Data}}
	case *DataPart:
		if v != nil {
			p.Content = &a2av1.Part_Data{Data: &a2av1.DataPart{Data: v.Data}}
		}
	}
	return p
}

// fromProtoPart 将 proto 转换为 Part 接口。
func fromProtoPart(part *a2av1.Part) Part {
	if part == nil {
		return nil
	}
	switch c := part.Content.(type) {
	case *a2av1.Part_Text:
		if c.Text != nil {
			return TextPart{TypeField: "text", Text: c.Text.Text}
		}
	case *a2av1.Part_File:
		if c.File != nil {
			return fromProtoFilePart(c.File)
		}
	case *a2av1.Part_Data:
		if c.Data != nil {
			return DataPart{TypeField: "data", Data: c.Data.Data}
		}
	}
	return TextPart{TypeField: part.Type}
}

func toProtoFilePart(fp FilePart) *a2av1.FilePart {
	protoFP := &a2av1.FilePart{
		Mimetype: fp.MimeType,
		Filename: fp.Filename,
	}
	if fp.File != nil {
		protoFP.Source = &a2av1.FilePart_FileBytes{
			FileBytes: &a2av1.FileWithBytes{
				Name:     fp.File.Name,
				MimeType: fp.File.MimeType,
				Bytes:    []byte(fp.File.Bytes),
			},
		}
	} else if fp.FileURI != nil {
		protoFP.Source = &a2av1.FilePart_FileUri{
			FileUri: &a2av1.FileWithURI{
				Uri:      fp.FileURI.URI,
				MimeType: fp.FileURI.MimeType,
			},
		}
	}
	return protoFP
}

func fromProtoFilePart(fp *a2av1.FilePart) FilePart {
	if fp == nil {
		return FilePart{}
	}
	filePart := FilePart{
		TypeField: "file",
		MimeType:  fp.Mimetype,
		Filename:  fp.Filename,
	}
	switch s := fp.Source.(type) {
	case *a2av1.FilePart_FileBytes:
		if s.FileBytes != nil {
			filePart.File = &FileWithBytes{
				Name:     s.FileBytes.Name,
				MimeType: s.FileBytes.MimeType,
				Bytes:    string(s.FileBytes.Bytes),
			}
		}
	case *a2av1.FilePart_FileUri:
		if s.FileUri != nil {
			filePart.FileURI = &FileWithURI{
				URI:      s.FileUri.Uri,
				MimeType: s.FileUri.MimeType,
			}
		}
	}
	return filePart
}

// toProtoArtifact 将 Artifact 转换为 proto。
func toProtoArtifact(artifact Artifact) *a2av1.Artifact {
	return &a2av1.Artifact{
		ArtifactId: artifact.ArtifactID,
		Mimetype:   artifact.MimeType,
		Bytes:      artifact.Bytes,
		Uri:        artifact.URI,
		CreatedAt:  toProtoTimestamp(artifact.CreatedAt),
	}
}

// fromProtoArtifact 将 proto 转换为 Artifact。
func fromProtoArtifact(artifact *a2av1.Artifact) Artifact {
	if artifact == nil {
		return Artifact{}
	}
	return Artifact{
		ArtifactID: artifact.ArtifactId,
		MimeType:   artifact.Mimetype,
		Bytes:      artifact.Bytes,
		URI:        artifact.Uri,
		CreatedAt:  fromProtoTimestamp(artifact.CreatedAt),
	}
}

// toProtoTaskEvent 将 TaskEvent 转换为 proto。
func toProtoTaskEvent(event *TaskEvent) *a2av1.TaskEvent {
	if event == nil {
		return nil
	}
	protoEvent := &a2av1.TaskEvent{
		Type:      string(event.Type),
		TaskId:    event.TaskID,
		Timestamp: toProtoTimestamp(event.Timestamp),
		Message:   toProtoMessage(event.Message),
		Error:     event.Error,
	}
	if event.Artifact != nil {
		protoEvent.Artifact = toProtoArtifact(*event.Artifact)
	}
	if event.State != nil {
		protoEvent.State = string(*event.State)
	}
	return protoEvent
}

// fromProtoTaskEvent 将 proto 转换为 TaskEvent。
func fromProtoTaskEvent(event *a2av1.TaskEvent) *TaskEvent {
	if event == nil {
		return nil
	}
	ev := &TaskEvent{
		Type:      TaskEventType(event.Type),
		TaskID:    event.TaskId,
		Timestamp: fromProtoTimestamp(event.Timestamp),
		Message:   fromProtoMessage(event.Message),
		Artifact:  ptrArtifact(fromProtoArtifact(event.Artifact)),
		Error:     event.Error,
	}
	if event.State != "" {
		state := TaskState(event.State)
		ev.State = &state
	}
	return ev
}

func ptrArtifact(a Artifact) *Artifact {
	if a.ArtifactID == "" && a.MimeType == "" && len(a.Bytes) == 0 && a.URI == "" && a.CreatedAt.IsZero() {
		return nil
	}
	return &a
}
