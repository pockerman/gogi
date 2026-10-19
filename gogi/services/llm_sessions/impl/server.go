package impl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gogi/gogi/applications"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/sessions"
	"gogi/gogi/utils"
	"regexp"
	"strings"
	"time"

	Log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LLMSessionServer implements the session service.
// The server provides the following endpoints
//   - GetOrCreateSession, ListSessions and DeleteSession manage the sessions of users
//   - AddMessages and GetMessages store and return the messages of a session; GetMessages
//     can also assemble the context of the next model call within a token budget
//   - SaveMemory, GetMemory, DeleteMemory and ClearUserMemory manage the facts saved about users

const (
	maxMessagesPerCall = 1000
	maxIDLength        = 256
	maxMemoryKeyLength = 256
	maxMemoryValueSize = 64 * 1024
)

// up to 128 characters as the application gives it, and its application's name and "." as it is kept
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,168}$`)

var roles = []string{sessions.RoleSystem, sessions.RoleUser, sessions.RoleAssistant, sessions.RoleTool}

// Config configures the session service
type Config struct {
	// DefaultTokenBudget is the budget of a context strategy when the request gives none
	DefaultTokenBudget int
}

func DefaultConfig() Config {
	return Config{DefaultTokenBudget: 8000}
}

type LLMSessionServer struct {
	gogiv1.UnimplementedLLMSessionServerServer
	store     sessions.Store
	assembler *sessions.Assembler
	config    Config
	now       func() time.Time
}

func NewLLMSessionServer(store sessions.Store, assembler *sessions.Assembler, config Config) *LLMSessionServer {
	return &LLMSessionServer{store: store, assembler: assembler, config: config, now: time.Now}
}

// storeError maps a store error to a gRPC status error
func storeError(err error, action string) error {
	switch {
	case errors.Is(err, sessions.ErrSessionNotFound):
		return status.Errorf(codes.NotFound, "%v", err)
	case errors.Is(err, sessions.ErrSessionOfAnotherUser):
		return status.Errorf(codes.PermissionDenied, "%v", err)
	case errors.Is(err, sessions.ErrMemoryConflict):
		return status.Errorf(codes.Aborted, "%v; read it again and retry", err)
	}
	return status.Errorf(codes.Internal, "failed to %s: %v", action, err)
}

func checkUserID(userID string) error {
	if strings.TrimSpace(userID) == "" {
		return status.Error(codes.InvalidArgument, "user_id is required")
	}
	if len(userID) > maxIDLength {
		return status.Errorf(codes.InvalidArgument, "user_id must be at most %d characters", maxIDLength)
	}
	return nil
}

func checkSessionID(sessionID string) error {
	if !sessionIDPattern.MatchString(sessionID) {
		return status.Errorf(codes.InvalidArgument,
			"session_id %q must be up to 128 letters, digits, '.', '_', ':' or '-', starting with a letter or digit", sessionID)
	}
	return nil
}

func toGRPCSession(session sessions.Session) *gogiv1.Session {
	return &gogiv1.Session{
		SessionId:    session.ID,
		UserId:       session.UserID,
		CreatedAt:    session.CreatedAt.UnixMilli(),
		UpdatedAt:    session.UpdatedAt.UnixMilli(),
		MessageCount: int32(session.MessageCount),
	}
}

func (s *LLMSessionServer) GetOrCreateSession(ctx context.Context, req *gogiv1.GetOrCreateSessionRequest) (*gogiv1.GetOrCreateSessionResponse, error) {

	if err := checkUserID(req.GetUserId()); err != nil {
		return nil, err
	}
	if req.SessionId != nil {
		if err := checkSessionID(req.GetSessionId()); err != nil {
			return nil, err
		}
	}

	// a new session's id is the application's, like those the application chooses
	sessionID := req.GetSessionId()
	if sessionID == "" {
		sessionID = applications.Qualify(applications.FromContext(ctx), utils.NewUUIDString())
	}
	session, created, err := s.store.GetOrCreateSession(ctx, req.GetUserId(), sessionID)
	if err != nil {
		return nil, storeError(err, "get or create the session")
	}
	return &gogiv1.GetOrCreateSessionResponse{Session: toGRPCSession(session), Created: created}, nil
}

func (s *LLMSessionServer) ListSessions(ctx context.Context, req *gogiv1.ListSessionsRequest) (*gogiv1.ListSessionsResponse, error) {

	if err := checkUserID(req.GetUserId()); err != nil {
		return nil, err
	}
	if req.GetLimit() < 0 || req.GetOffset() < 0 {
		return nil, status.Error(codes.InvalidArgument, "limit and offset must not be negative")
	}

	list, total, err := s.store.ListSessions(ctx, req.GetUserId(), int(req.GetLimit()), int(req.GetOffset()))
	if err != nil {
		return nil, storeError(err, "list the sessions")
	}
	converted := make([]*gogiv1.Session, 0, len(list))
	for _, session := range list {
		converted = append(converted, toGRPCSession(session))
	}
	return &gogiv1.ListSessionsResponse{Sessions: converted, TotalCount: int32(total)}, nil
}

func (s *LLMSessionServer) DeleteSession(ctx context.Context, req *gogiv1.DeleteSessionRequest) (*gogiv1.DeleteSessionResponse, error) {

	if err := checkSessionID(req.GetSessionId()); err != nil {
		return nil, err
	}
	deleted, err := s.store.DeleteSession(ctx, req.GetSessionId(), req.GetDeleteMemories())
	if err != nil {
		return nil, storeError(err, "delete the session")
	}
	return &gogiv1.DeleteSessionResponse{Success: deleted}, nil
}

// toMessage converts and checks a message: a message has a known role, a tool result says
// which call it answers, and an assistant message has content or tool calls
func (s *LLMSessionServer) toMessage(message *gogiv1.LLMMessage, index int) (sessions.Message, error) {

	invalid := func(format string, args ...any) error {
		return status.Errorf(codes.InvalidArgument, "message %d: %s", index, fmt.Sprintf(format, args...))
	}
	converted := sessions.Message{Role: message.GetRole(), Content: message.Content, ToolCallID: message.ToolCallId,
		Name: message.Name, Timestamp: message.GetTimestamp()}

	switch converted.Role {
	case sessions.RoleSystem, sessions.RoleUser:
		if message.Content == nil {
			return converted, invalid("a %s message needs content", converted.Role)
		}
	case sessions.RoleTool:
		if message.GetToolCallId() == "" {
			return converted, invalid("a tool message needs the tool_call_id of the call it answers")
		}
	case sessions.RoleAssistant:
		if message.Content == nil && len(message.GetToolCalls()) == 0 {
			return converted, invalid("an assistant message needs content or tool calls")
		}
	default:
		return converted, invalid("role %q must be one of %s", converted.Role, strings.Join(roles, ", "))
	}

	if len(message.GetToolCalls()) > 0 && converted.Role != sessions.RoleAssistant {
		return converted, invalid("only assistant messages call tools")
	}
	for i, call := range message.GetToolCalls() {
		if call.GetId() == "" || call.GetFunction().GetName() == "" {
			return converted, invalid("tool call %d needs an id and a function name", i)
		}
		callType := call.GetType()
		if callType == "" {
			callType = "function"
		}
		converted.ToolCalls = append(converted.ToolCalls, sessions.ToolCall{ID: call.GetId(), Type: callType,
			Name: call.GetFunction().GetName(), Arguments: call.GetFunction().GetArguments()})
	}

	if converted.Timestamp == 0 {
		converted.Timestamp = s.now().UnixMilli()
	}
	return converted, nil
}

func toGRPCMessage(message sessions.Message) *gogiv1.LLMMessage {
	converted := &gogiv1.LLMMessage{Role: message.Role, Content: message.Content, ToolCallId: message.ToolCallID,
		Name: message.Name, Timestamp: message.Timestamp}
	for _, call := range message.ToolCalls {
		converted.ToolCalls = append(converted.ToolCalls, &gogiv1.ToolCall{Id: call.ID, Type: call.Type,
			Function: &gogiv1.ToolCallFunction{Name: call.Name, Arguments: call.Arguments}})
	}
	return converted
}

func toGRPCMessages(messages []sessions.Message) []*gogiv1.LLMMessage {
	converted := make([]*gogiv1.LLMMessage, 0, len(messages))
	for _, message := range messages {
		converted = append(converted, toGRPCMessage(message))
	}
	return converted
}

func (s *LLMSessionServer) AddMessages(ctx context.Context, req *gogiv1.AddMessagesRequest) (*gogiv1.AddMessagesResponse, error) {

	if err := checkSessionID(req.GetSessionId()); err != nil {
		return nil, err
	}
	switch count := len(req.GetMessages()); {
	case count == 0:
		return nil, status.Error(codes.InvalidArgument, "messages are required")
	case count > maxMessagesPerCall:
		return nil, status.Errorf(codes.InvalidArgument, "at most %d messages can be added at once", maxMessagesPerCall)
	}

	messages := make([]sessions.Message, 0, len(req.GetMessages()))
	for i, message := range req.GetMessages() {
		converted, err := s.toMessage(message, i)
		if err != nil {
			return nil, err
		}
		messages = append(messages, converted)
	}

	total, err := s.store.AddMessages(ctx, req.GetSessionId(), messages)
	if err != nil {
		return nil, storeError(err, "add the messages")
	}
	return &gogiv1.AddMessagesResponse{Success: true, MessageCount: int32(len(messages)), TotalCount: int32(total)}, nil
}

func (s *LLMSessionServer) GetMessages(ctx context.Context, req *gogiv1.GetMessagesRequest) (*gogiv1.GetMessagesResponse, error) {

	if err := checkSessionID(req.GetSessionId()); err != nil {
		return nil, err
	}
	if req.GetLimit() < 0 || req.GetOffset() < 0 {
		return nil, status.Error(codes.InvalidArgument, "limit and offset must not be negative")
	}

	strategy := strings.ToLower(strings.TrimSpace(req.GetStrategy()))
	if strategy == "" {
		strategy = sessions.StrategyNone
	}
	if !contains(sessions.Strategies, strategy) {
		return nil, status.Errorf(codes.InvalidArgument, "strategy %q must be one of %s", req.GetStrategy(),
			strings.Join(sessions.Strategies, ", "))
	}

	if strategy == sessions.StrategyNone {
		messages, total, err := s.store.GetMessages(ctx, req.GetSessionId(), int(req.GetLimit()), int(req.GetOffset()))
		if err != nil {
			return nil, storeError(err, "get the messages")
		}
		return &gogiv1.GetMessagesResponse{Messages: toGRPCMessages(messages), TotalCount: int32(total),
			Strategy: strategy, EstimatedTokens: int32(s.assembler.Estimator.Messages(messages)), Notes: []string{}}, nil
	}

	budget := s.config.DefaultTokenBudget
	if req.TokenBudget != nil {
		if req.GetTokenBudget() <= 0 {
			return nil, status.Error(codes.InvalidArgument, "token_budget must be positive")
		}
		budget = int(req.GetTokenBudget())
	}
	if req.GetOffset() > 0 {
		return nil, status.Errorf(codes.InvalidArgument, "offset only applies without a strategy")
	}

	session, err := s.store.GetSession(ctx, req.GetSessionId())
	if err != nil {
		return nil, storeError(err, "get the session")
	}
	messages, total, err := s.store.GetMessages(ctx, req.GetSessionId(), 0, 0)
	if err != nil {
		return nil, storeError(err, "get the messages")
	}

	result, err := s.assembler.Assemble(ctx, messages, sessions.ContextRequest{SessionID: session.ID,
		UserID: session.UserID, Strategy: strategy, Budget: budget, Limit: int(req.GetLimit()),
		Focus: req.GetSummaryFocus()})
	if err != nil {
		return nil, storeError(err, "assemble the context")
	}

	return &gogiv1.GetMessagesResponse{
		Messages:        toGRPCMessages(result.Messages),
		TotalCount:      int32(total),
		Strategy:        result.Strategy,
		OmittedCount:    int32(result.Omitted),
		SummarizedCount: int32(result.Summarized),
		Summary:         result.Summary,
		EstimatedTokens: int32(result.Tokens),
		Notes:           result.Notes,
	}, nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func checkMemoryKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return status.Error(codes.InvalidArgument, "key is required")
	}
	if len(key) > maxMemoryKeyLength {
		return status.Errorf(codes.InvalidArgument, "key must be at most %d characters", maxMemoryKeyLength)
	}
	return nil
}

func toGRPCMemory(memory sessions.Memory) *gogiv1.MemoryEntry {
	return &gogiv1.MemoryEntry{Key: memory.Key, Value: memory.Value, SessionId: memory.SessionID,
		CreatedAt: memory.CreatedAt, UpdatedAt: memory.UpdatedAt}
}

// SaveMemory saves a fact about a user, for every session or for one of the user's sessions.
// Every save is logged, without its value, so that it is known why the system knows a fact
func (s *LLMSessionServer) SaveMemory(ctx context.Context, req *gogiv1.SaveMemoryRequest) (*gogiv1.SaveMemoryResponse, error) {

	if err := checkUserID(req.GetUserId()); err != nil {
		return nil, err
	}
	if err := checkMemoryKey(req.GetKey()); err != nil {
		return nil, err
	}
	if len(req.GetValue()) > maxMemoryValueSize {
		return nil, status.Errorf(codes.InvalidArgument, "value must be at most %d bytes", maxMemoryValueSize)
	}
	if !json.Valid([]byte(req.GetValue())) {
		return nil, status.Error(codes.InvalidArgument, `value must be a JSON document, e.g. "\"window seat\"" for a string`)
	}

	if req.SessionId != nil {
		session, err := s.store.GetSession(ctx, req.GetSessionId())
		if err != nil {
			return nil, storeError(err, "get the session")
		}
		if session.UserID != req.GetUserId() {
			return nil, status.Errorf(codes.PermissionDenied, "%v: %s", sessions.ErrSessionOfAnotherUser, session.ID)
		}
	}
	if req.ExpectedUpdatedAt != nil && req.GetExpectedUpdatedAt() < 0 {
		return nil, status.Error(codes.InvalidArgument, "expected_updated_at must not be negative")
	}

	saved, err := s.store.SaveMemory(ctx, sessions.Memory{UserID: req.GetUserId(), SessionID: req.GetSessionId(),
		Key: req.GetKey(), Value: req.GetValue()}, req.ExpectedUpdatedAt)
	if err != nil {
		return nil, storeError(err, "save the memory")
	}
	Log.Infof("memory %q saved for user %s (session %q)", saved.Key, saved.UserID, saved.SessionID)
	return &gogiv1.SaveMemoryResponse{Success: true, Entry: toGRPCMemory(saved)}, nil
}

func (s *LLMSessionServer) GetMemory(ctx context.Context, req *gogiv1.GetMemoryRequest) (*gogiv1.GetMemoryResponse, error) {

	if err := checkUserID(req.GetUserId()); err != nil {
		return nil, err
	}
	memories, err := s.store.GetMemories(ctx, req.GetUserId(), req.GetKey(), req.SessionId)
	if err != nil {
		return nil, storeError(err, "get the memories")
	}

	// a key saved in several scopes maps to its most recently updated value; on a tie, to
	// a session's, the more specific
	values := map[string]string{}
	updated := map[string]int64{}
	entries := make([]*gogiv1.MemoryEntry, 0, len(memories))
	for _, memory := range memories {
		at, seen := updated[memory.Key]
		if !seen || memory.UpdatedAt > at || (memory.UpdatedAt == at && memory.SessionID != "") {
			values[memory.Key], updated[memory.Key] = memory.Value, memory.UpdatedAt
		}
		entries = append(entries, toGRPCMemory(memory))
	}
	return &gogiv1.GetMemoryResponse{Memories: values, Entries: entries}, nil
}

func (s *LLMSessionServer) DeleteMemory(ctx context.Context, req *gogiv1.DeleteMemoryRequest) (*gogiv1.DeleteMemoryResponse, error) {

	if err := checkUserID(req.GetUserId()); err != nil {
		return nil, err
	}
	if err := checkMemoryKey(req.GetKey()); err != nil {
		return nil, err
	}
	count, err := s.store.DeleteMemories(ctx, req.GetUserId(), req.GetKey(), req.SessionId)
	if err != nil {
		return nil, storeError(err, "delete the memory")
	}
	if count > 0 {
		Log.Infof("memory %q deleted for user %s (%d entries)", req.GetKey(), req.GetUserId(), count)
	}
	return &gogiv1.DeleteMemoryResponse{Success: count > 0, Count: int32(count)}, nil
}

func (s *LLMSessionServer) ClearUserMemory(ctx context.Context, req *gogiv1.ClearUserMemoryRequest) (*gogiv1.ClearUserMemoryResponse, error) {

	if err := checkUserID(req.GetUserId()); err != nil {
		return nil, err
	}
	count, err := s.store.ClearUserMemory(ctx, req.GetUserId())
	if err != nil {
		return nil, storeError(err, "clear the memories")
	}
	Log.Infof("memories of user %s cleared (%d entries)", req.GetUserId(), count)
	return &gogiv1.ClearUserMemoryResponse{Count: int32(count)}, nil
}
