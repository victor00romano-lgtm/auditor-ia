package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type ConversationStatus string

const (
	ConversationNoConversation ConversationStatus = "NO_CONVERSATION"
	ConversationEmpty          ConversationStatus = "EMPTY_CONVERSATION"
	ConversationAccessDenied   ConversationStatus = "ACCESS_DENIED"
	ConversationAvailable      ConversationStatus = "AVAILABLE"
)

var ErrConversationAccessDenied = errors.New("acesso ao histórico da conversa negado")

const MaxAnalyzedMessages = 50

type ConversationAccessError struct {
	SessionID string
	Code      string
}

func (e *ConversationAccessError) Error() string {
	return fmt.Sprintf("histórico da sessão %s indisponível: %s", e.SessionID, e.Code)
}

func (e *ConversationAccessError) Is(target error) bool { return target == ErrConversationAccessDenied }

type Conversation struct {
	Status    ConversationStatus
	Messages  []Message
	SessionID string
	ErrorCode string
}

type Message struct {
	ID          string
	SessionID   string
	SenderID    string
	SenderName  string
	CreatedAt   time.Time
	Text        string
	Role        string
	ChatOrigin  string
	Attachments []Attachment
}

type Attachment struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MIMEType string `json:"mime_type"`
	IsImage  bool   `json:"is_image"`
}
type Source interface {
	Messages(context.Context, string) ([]Message, error)
}

type ConversationSource interface {
	Conversation(context.Context, string) (Conversation, error)
}

type CRMFormField struct {
	Type    string
	Code    string
	Caption string
	Value   any
}

type CRMForm struct {
	ActivityID   string
	Form         any
	VisitedPages any
	Fields       []CRMFormField
}

type CRMFormSource interface {
	Forms(context.Context, string) ([]CRMForm, error)
}

type MessageRepository interface {
	UpsertBatch(context.Context, int64, []Message) (MessageUpsertResult, error)
}

type MessageUpsertResult struct {
	Processed int
}

type MessagePersistenceError struct {
	Operation string
	Cause     error
}

func (e *MessagePersistenceError) Error() string {
	return "persistência obrigatória de mensagens falhou"
}
func (e *MessagePersistenceError) Unwrap() error { return e.Cause }

type DealStatus struct {
	StageID      string
	SemanticID   string
	Closed       bool
	AssignedByID int64
}

type DealStatusSource interface {
	Status(context.Context, string) (DealStatus, error)
}

type AssigneeRoleSource interface {
	IsSupport(context.Context, int64) (bool, error)
}

type Analyzer interface {
	Analyze(context.Context, string, []Message, []CRMForm) (ConversationAnalysis, error)
}
type Result struct {
	DealID                string
	MessageCount          int
	CollectedMessageCount int
	PersistedMessageCount int
	AnalyzedMessageCount  int
	Analysis              string
	FinalResult           string
	ResultSource          string
	ConversationState     string
	Observation           string
	RecommendedAction     string
	AnalysisSource        string
	FormCount             int
	FormFieldCount        int
	StructuredAnalysis    ConversationAnalysis
	ConversationStatus    ConversationStatus
	SessionID             string
	ConversationError     string
}
