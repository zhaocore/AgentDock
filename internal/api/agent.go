package api

import (
	"database/sql"
	"errors"
	"log"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/zhaocore/agentdock/internal/domain"
	"github.com/zhaocore/agentdock/internal/store/mysql"
)

// AgentRequest Agent 创建/更新请求。
type AgentRequest struct {
	Name         string                   `json:"name" binding:"required"`
	SystemPrompt string                   `json:"system_prompt" binding:"required"`
	Model        string                   `json:"model" binding:"required"`
	Skills       []string                 `json:"skills"`
	MCPServers   []domain.MCPServerConfig `json:"mcp_servers"`
	Subagents    []domain.SubagentDef     `json:"subagents"`
}

// CreateInstanceRequest 创建实例请求。
type CreateInstanceRequest struct {
	RepoIDs []string `json:"repo_ids"`
}

// SendMessageRequest 发送消息请求。
type SendMessageRequest struct {
	Content string `json:"content" binding:"required"`
}

// toAgent 把请求体转换为领域对象（补全空集合）。
func toAgent(req AgentRequest, owner string) domain.Agent {
	agent := domain.Agent{
		OwnerUserID:  owner,
		Name:         req.Name,
		SystemPrompt: req.SystemPrompt,
		Model:        req.Model,
		Skills:       req.Skills,
		MCPServers:   req.MCPServers,
		Subagents:    req.Subagents,
	}
	if agent.Skills == nil {
		agent.Skills = []string{}
	}
	if agent.MCPServers == nil {
		agent.MCPServers = []domain.MCPServerConfig{}
	}
	if agent.Subagents == nil {
		agent.Subagents = []domain.SubagentDef{}
	}
	return agent
}

// @Summary 创建 Agent
// @Tags Agents
// @Security JWTAuth
// @Accept json
// @Produce json
// @Param body body AgentRequest true "Agent 定义"
// @Success 201 {object} domain.Agent
// @Failure 400 {object} ErrorResponse
// @Router /v1/agents [post]
func (h *Handler) CreateAgent(c *gin.Context) {
	var req AgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	agent := toAgent(req, c.GetString("user_id"))
	agent.ID = uuid.New().String()
	if err := mysql.CreateAgent(c.Request.Context(), h.DB, agent); err != nil {
		log.Printf("create agent: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create agent"})
		return
	}
	c.JSON(http.StatusCreated, agent)
}

// @Summary Agent 列表
// @Description 同步部署配置并返回公共预设与当前用户的私有 Agent。
// @Tags Agents
// @Security JWTAuth
// @Produce json
// @Param limit query int false "分页大小"
// @Param offset query int false "偏移"
// @Success 200 {object} PaginatedResponse
// @Router /v1/agents [get]
func (h *Handler) ListAgents(c *gin.Context) {
	limit, offset, err := parsePagination(c.Request)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if h.SyncAgents != nil {
		if err := h.SyncAgents(c.Request.Context()); err != nil {
			slog.Error("sync public agents", "error", err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to sync public agents; deployer must check agent configuration"})
			return
		}
	}
	list, total, err := mysql.ListAgents(c.Request.Context(), h.DB, c.GetString("user_id"), limit, offset)
	if err != nil {
		log.Printf("list agents: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to list agents"})
		return
	}
	views := make([]domain.Agent, 0, len(list))
	for _, agent := range list {
		views = append(views, agentView(agent))
	}
	c.JSON(http.StatusOK, NewPaginatedResponse(views, total, limit, offset))
}

// agentView 公共定义仅暴露 MCP 名称和传输类型，隐藏部署连接信息。
func agentView(agent domain.Agent) domain.Agent {
	if agent.OwnerUserID == "" {
		servers := make([]domain.MCPServerConfig, 0, len(agent.MCPServers))
		for _, server := range agent.MCPServers {
			servers = append(servers, domain.MCPServerConfig{Name: server.Name, Transport: server.Transport})
		}
		agent.MCPServers = servers
	}
	return agent
}

// loadAgent 读取公共预设或当前用户私有定义，未命中写响应并返回 false。
func (h *Handler) loadAgent(c *gin.Context) (domain.Agent, bool) {
	agentID := c.Param("agent_id")
	if !validateID(c, agentID, "agent_id") {
		return domain.Agent{}, false
	}
	agent, err := mysql.GetAgent(c.Request.Context(), h.DB, agentID, c.GetString("user_id"))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "agent not found"})
		return domain.Agent{}, false
	}
	if err != nil {
		log.Printf("get agent: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to load agent"})
		return domain.Agent{}, false
	}
	return agent, true
}

// @Summary Agent 详情
// @Tags Agents
// @Security JWTAuth
// @Produce json
// @Param agent_id path string true "Agent ID"
// @Success 200 {object} domain.Agent
// @Failure 404 {object} ErrorResponse
// @Router /v1/agents/{agent_id} [get]
func (h *Handler) GetAgent(c *gin.Context) {
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, agentView(agent))
}

// @Summary 更新 Agent
// @Tags Agents
// @Security JWTAuth
// @Accept json
// @Produce json
// @Param agent_id path string true "Agent ID"
// @Param body body AgentRequest true "Agent 定义"
// @Success 200 {object} domain.Agent
// @Failure 404 {object} ErrorResponse
// @Router /v1/agents/{agent_id} [put]
func (h *Handler) UpdateAgent(c *gin.Context) {
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	if agent.OwnerUserID == "" {
		c.JSON(http.StatusForbidden, ErrorResponse{Error: "public agents are read-only; deployer must edit preset files"})
		return
	}
	var req AgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	updated := toAgent(req, agent.OwnerUserID)
	updated.ID = agent.ID
	if err := mysql.UpdateAgent(c.Request.Context(), h.DB, updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "agent not found"})
			return
		}
		log.Printf("update agent: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to update agent"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// @Summary 删除 Agent
// @Description 删除 Agent 及其实例与会话（级联）。
// @Tags Agents
// @Security JWTAuth
// @Param agent_id path string true "Agent ID"
// @Success 204
// @Failure 404 {object} ErrorResponse
// @Router /v1/agents/{agent_id} [delete]
func (h *Handler) DeleteAgent(c *gin.Context) {
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	if agent.OwnerUserID == "" {
		c.JSON(http.StatusForbidden, ErrorResponse{Error: "public agents are read-only; deployer must remove preset files"})
		return
	}
	if err := mysql.DeleteAgent(c.Request.Context(), h.DB, agent.ID, agent.OwnerUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "agent not found"})
			return
		}
		log.Printf("delete agent: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to delete agent"})
		return
	}
	c.Status(http.StatusNoContent)
}

// @Summary 创建 Agent 实例
// @Tags Instances
// @Security JWTAuth
// @Accept json
// @Produce json
// @Param agent_id path string true "Agent ID"
// @Param body body CreateInstanceRequest true "实例参数"
// @Success 201 {object} domain.AgentInstance
// @Failure 404 {object} ErrorResponse
// @Router /v1/agents/{agent_id}/instances [post]
func (h *Handler) CreateInstance(c *gin.Context) {
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	var req CreateInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.RepoIDs == nil {
		req.RepoIDs = []string{}
	}
	inst := domain.AgentInstance{
		ID:          uuid.New().String(),
		AgentID:     agent.ID,
		OwnerUserID: c.GetString("user_id"),
		Status:      domain.AgentStatusCreated,
		RepoIDs:     req.RepoIDs,
	}
	if err := mysql.CreateInstance(c.Request.Context(), h.DB, inst); err != nil {
		log.Printf("create instance: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create instance"})
		return
	}
	c.JSON(http.StatusCreated, inst)
}

// @Summary 实例列表
// @Tags Instances
// @Security JWTAuth
// @Produce json
// @Param agent_id path string true "Agent ID"
// @Success 200 {object} map[string]any
// @Failure 404 {object} ErrorResponse
// @Router /v1/agents/{agent_id}/instances [get]
func (h *Handler) ListInstances(c *gin.Context) {
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	list, err := mysql.ListInstances(c.Request.Context(), h.DB, agent.ID, c.GetString("user_id"))
	if err != nil {
		log.Printf("list instances: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to list instances"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// loadOwnedInstance 取当前用户实例，允许停止已停用预设的运行实例。
func (h *Handler) loadOwnedInstance(c *gin.Context) (domain.AgentInstance, bool) {
	instanceID := c.Param("instance_id")
	if !validateID(c, instanceID, "instance_id") {
		return domain.AgentInstance{}, false
	}
	inst, err := mysql.GetInstance(c.Request.Context(), h.DB, instanceID, c.GetString("user_id"))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "instance not found"})
		return domain.AgentInstance{}, false
	}
	if err != nil {
		log.Printf("get instance: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to load instance"})
		return domain.AgentInstance{}, false
	}
	return inst, true
}

// loadInstance 取当前用户实例和仍启用的 Agent 定义。
func (h *Handler) loadInstance(c *gin.Context) (domain.AgentInstance, domain.Agent, bool) {
	inst, ok := h.loadOwnedInstance(c)
	if !ok {
		return domain.AgentInstance{}, domain.Agent{}, false
	}
	agent, err := mysql.GetAgent(c.Request.Context(), h.DB, inst.AgentID, inst.OwnerUserID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusConflict, ErrorResponse{Error: "agent definition is no longer available; deployer must restore the preset"})
		return domain.AgentInstance{}, domain.Agent{}, false
	}
	if err != nil {
		log.Printf("get agent for instance: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to load instance"})
		return domain.AgentInstance{}, domain.Agent{}, false
	}
	return inst, agent, true
}

// @Summary 启动实例
// @Tags Instances
// @Security JWTAuth
// @Param instance_id path string true "实例 ID"
// @Success 202
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Router /v1/instances/{instance_id}/start [post]
func (h *Handler) StartInstance(c *gin.Context) {
	inst, agent, ok := h.loadInstance(c)
	if !ok {
		return
	}
	if err := h.Instances.Start(c.Request.Context(), inst, agent); err != nil {
		c.JSON(http.StatusConflict, ErrorResponse{Error: err.Error()})
		return
	}
	c.Status(http.StatusAccepted)
}

// @Summary 停止实例
// @Tags Instances
// @Security JWTAuth
// @Param instance_id path string true "实例 ID"
// @Success 202
// @Failure 404 {object} ErrorResponse
// @Router /v1/instances/{instance_id}/stop [post]
func (h *Handler) StopInstance(c *gin.Context) {
	inst, ok := h.loadOwnedInstance(c)
	if !ok {
		return
	}
	if err := h.Instances.Stop(c.Request.Context(), inst.ID); err != nil {
		log.Printf("stop instance: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to stop instance"})
		return
	}
	c.Status(http.StatusAccepted)
}

// @Summary 创建会话
// @Tags Sessions
// @Security JWTAuth
// @Produce json
// @Param instance_id path string true "实例 ID"
// @Success 201 {object} domain.Session
// @Failure 404 {object} ErrorResponse
// @Router /v1/instances/{instance_id}/sessions [post]
func (h *Handler) CreateSession(c *gin.Context) {
	inst, _, ok := h.loadInstance(c)
	if !ok {
		return
	}
	session := domain.Session{
		ID:         uuid.New().String(),
		UserID:     c.GetString("user_id"),
		InstanceID: inst.ID,
		AgentID:    inst.AgentID,
		Title:      "新会话",
	}
	if err := mysql.CreateSession(c.Request.Context(), h.DB, session); err != nil {
		log.Printf("create session: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create session"})
		return
	}
	c.JSON(http.StatusCreated, session)
}

// @Summary 发送消息
// @Description 在会话上执行一轮 Agent 对话；要求实例处于运行状态。
// @Tags Sessions
// @Security JWTAuth
// @Accept json
// @Produce json
// @Param session_id path string true "会话 ID"
// @Param body body SendMessageRequest true "消息内容"
// @Success 200 {object} domain.MessageView
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Router /v1/sessions/{session_id}/messages [post]
func (h *Handler) SendMessage(c *gin.Context) {
	sessionID := c.Param("session_id")
	if !validateID(c, sessionID, "session_id") {
		return
	}
	var req SendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	session, err := mysql.GetSession(c.Request.Context(), h.DB, sessionID, c.GetString("user_id"))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "session not found"})
		return
	}
	if err != nil {
		log.Printf("get session: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to send message"})
		return
	}
	inst, err := mysql.GetInstance(c.Request.Context(), h.DB, session.InstanceID, session.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "instance not found"})
		return
	}
	if err != nil {
		log.Printf("get instance: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to send message"})
		return
	}
	agent, err := mysql.GetAgent(c.Request.Context(), h.DB, session.AgentID, session.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "agent not found"})
		return
	}
	if err != nil {
		log.Printf("get agent: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to send message"})
		return
	}
	if !h.Instances.IsRunning(inst.ID) {
		c.JSON(http.StatusConflict, ErrorResponse{Error: "instance is not running"})
		return
	}

	// 持久化用户消息
	userMsg := domain.Message{
		ID: uuid.New().String(), SessionID: session.ID, Role: "user", Content: req.Content,
	}
	if err := mysql.InsertMessage(c.Request.Context(), h.DB, userMsg); err != nil {
		log.Printf("insert user message: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to send message"})
		return
	}

	history, err := mysql.ListMessages(c.Request.Context(), h.DB, session.ID)
	if err != nil {
		log.Printf("list history: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to send message"})
		return
	}

	answer, meta, err := h.Instances.SendMessage(c.Request.Context(), inst, agent, history, req.Content)
	if err != nil {
		log.Printf("agent execute: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "agent execution failed"})
		return
	}
	assistantMsg := domain.Message{
		ID: uuid.New().String(), SessionID: session.ID, Role: "assistant", Content: answer, AgentMeta: meta,
	}
	if err := mysql.InsertMessage(c.Request.Context(), h.DB, assistantMsg); err != nil {
		log.Printf("insert assistant message: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to send message"})
		return
	}
	c.JSON(http.StatusOK, assistantMsg)
}

// @Summary 会话列表
// @Description 当前用户的会话分页列表，按更新时间倒序。
// @Tags Sessions
// @Security JWTAuth
// @Produce json
// @Param limit query int false "分页大小"
// @Param offset query int false "偏移"
// @Success 200 {object} PaginatedResponse
// @Router /v1/sessions [get]
func (h *Handler) ListSessions(c *gin.Context) {
	limit, offset, err := parsePagination(c.Request)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	list, total, err := mysql.ListSessions(c.Request.Context(), h.DB, c.GetString("user_id"), limit, offset)
	if err != nil {
		log.Printf("list sessions: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to list sessions"})
		return
	}
	c.JSON(http.StatusOK, NewPaginatedResponse(list, total, limit, offset))
}

// @Summary 会话消息历史
// @Tags Sessions
// @Security JWTAuth
// @Produce json
// @Param session_id path string true "会话 ID"
// @Success 200 {object} map[string]any
// @Failure 404 {object} ErrorResponse
// @Router /v1/sessions/{session_id}/messages [get]
func (h *Handler) ListSessionMessages(c *gin.Context) {
	sessionID := c.Param("session_id")
	if !validateID(c, sessionID, "session_id") {
		return
	}
	if _, err := mysql.GetSession(c.Request.Context(), h.DB, sessionID, c.GetString("user_id")); errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "session not found"})
		return
	} else if err != nil {
		log.Printf("get session: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to list messages"})
		return
	}
	messages, err := mysql.ListMessages(c.Request.Context(), h.DB, sessionID)
	if err != nil {
		log.Printf("list messages: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to list messages"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": messages})
}

// @Summary 删除会话
// @Description 软删除当前用户的会话。
// @Tags Sessions
// @Security JWTAuth
// @Param session_id path string true "会话 ID"
// @Success 204
// @Failure 404 {object} ErrorResponse
// @Router /v1/sessions/{session_id} [delete]
func (h *Handler) DeleteSession(c *gin.Context) {
	sessionID := c.Param("session_id")
	if !validateID(c, sessionID, "session_id") {
		return
	}
	if err := mysql.DeleteSession(c.Request.Context(), h.DB, sessionID, c.GetString("user_id")); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "session not found"})
			return
		}
		log.Printf("delete session: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to delete session"})
		return
	}
	c.Status(http.StatusNoContent)
}
