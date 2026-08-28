package main

// chat_jobs.go — jobs de chat em background (FASE MAIOR, itens 2+3).
//
// O processamento do chat roda em goroutine própria com contexto INDEPENDENTE
// do request HTTP (context.Background + timeout próprio) — sobrevive à
// desconexão da aba/app. O frontend consulta o status via GET /chat/job e
// retoma o resultado ao voltar.
//
// Persistência: em memória (v1). Se o hokma reiniciar durante um job, o job
// morre — a resposta da sessão opencode serve continua no servidor; o
// frontend recupera via histórico da conversa (fallback documentado).

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type chatJobStatus string

const (
	chatJobRunning chatJobStatus = "running"
	chatJobDone    chatJobStatus = "done"
)

// chatJobAsyncTimeout — limite total do processamento em background.
const chatJobAsyncTimeout = 10 * time.Minute

type chatJob struct {
	ID         string
	ConvID     string
	TenantID   string
	UserID     string
	Status     chatJobStatus
	Reply      string
	Mode       string
	Skill      string
	Engine     string
	ModelUsed  string
	CreatedAt  time.Time
	FinishedAt time.Time
}

var (
	chatJobsMu sync.Mutex
	chatJobs   = map[string]*chatJob{}
)

// startChatJob inicia o processamento em background e devolve o job ID.
// A closure recebe um contexto próprio (desacoplado do request).
func startChatJob(convID, tenantID, userID string, run func(ctx context.Context) (reply, mode, skill, engine, modelUsed string)) string {
	job := &chatJob{
		ID:        fmt.Sprintf("job_%d", time.Now().UnixNano()),
		ConvID:    convID,
		TenantID:  tenantID,
		UserID:    userID,
		Status:    chatJobRunning,
		CreatedAt: time.Now(),
	}
	chatJobsMu.Lock()
	chatJobs[job.ID] = job
	chatJobsMu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), chatJobAsyncTimeout)
		defer cancel()
		reply, mode, skill, engine, modelUsed := run(ctx)
		chatJobsMu.Lock()
		job.Reply = reply
		job.Mode = mode
		job.Skill = skill
		job.Engine = engine
		job.ModelUsed = modelUsed
		job.Status = chatJobDone
		job.FinishedAt = time.Now()
		chatJobsMu.Unlock()
	}()
	return job.ID
}

func getChatJob(id string) *chatJob {
	chatJobsMu.Lock()
	defer chatJobsMu.Unlock()
	return chatJobs[id]
}

// listChatJobsByConv devolve os jobs da conversa (mais recente primeiro).
func listChatJobsByConv(convID string) []*chatJob {
	chatJobsMu.Lock()
	defer chatJobsMu.Unlock()
	var out []*chatJob
	for _, j := range chatJobs {
		if j.ConvID == convID {
			out = append(out, j)
		}
	}
	for i := 0; i < len(out); i++ {
		for k := i + 1; k < len(out); k++ {
			if out[k].CreatedAt.After(out[i].CreatedAt) {
				out[i], out[k] = out[k], out[i]
			}
		}
	}
	return out
}

func chatJobPublic(j *chatJob) map[string]interface{} {
	out := map[string]interface{}{
		"job_id":     j.ID,
		"conv_id":    j.ConvID,
		"status":     j.Status,
		"reply":      j.Reply,
		"mode":       j.Mode,
		"skill":      j.Skill,
		"engine":     j.Engine,
		"model_used": j.ModelUsed,
		"created_at": j.CreatedAt.Unix(),
		"finished_at": func() int64 {
			if j.FinishedAt.IsZero() {
				return 0
			}
			return j.FinishedAt.Unix()
		}(),
	}
	// Card de aprovação pendente da conversa (ex.: job que terminou em
	// opencode_serve_pending) — o frontend reexibe na retomada.
	if pa := getPendingAction(j.ConvID, j.TenantID, j.UserID); pa != nil {
		out["pending_action"] = pa
	}
	return out
}

// persistChatJobMessages grava a troca (usuário + assistente) em
// conv_messages — permite ao frontend recuperar a resposta ao voltar para a
// conversa (a persistência local do frontend não sobrevive à troca de aba).
func persistChatJobMessages(convID, userMsg, reply string) {
	if convID == "" {
		convID = defaultConvId
	}
	ts := time.Now().Unix()
	sqliteExecParams(
		`INSERT OR REPLACE INTO conv_messages (id, conv_id, role, content, ts) VALUES (?, ?, 'user', ?, ?)`,
		fmt.Sprintf("cjm_%d_u", ts), convID, userMsg, ts,
	)
	if reply != "" {
		sqliteExecParams(
			`INSERT OR REPLACE INTO conv_messages (id, conv_id, role, content, ts) VALUES (?, ?, 'assistant', ?, ?)`,
			fmt.Sprintf("cjm_%d_a", ts+1), convID, reply, ts+1,
		)
	}
}

// handleChatJob — GET /chat/job:
//
//	?conv_id=X → jobs da conversa (mais recente primeiro; o frontend usa para
//	             retomar/verificar trabalho em background ao voltar)
//	?id=job_X  → job específico (status + resultado quando done)
func handleChatJob(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		jsonError(w, "metodo nao suportado", http.StatusMethodNotAllowed)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	convID := r.URL.Query().Get("conv_id")
	id := r.URL.Query().Get("id")
	if id != "" {
		j := getChatJob(id)
		if j == nil {
			jsonError(w, "job nao encontrado", http.StatusNotFound)
			return
		}
		respondJSON(w, chatJobPublic(j))
		return
	}
	if convID == "" {
		jsonError(w, "conv_id ou id obrigatorio", http.StatusBadRequest)
		return
	}
	jobs := listChatJobsByConv(convID)
	out := make([]map[string]interface{}, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, chatJobPublic(j))
	}
	respondJSON(w, map[string]interface{}{"jobs": out})
}

func init() {
	http.HandleFunc("/chat/job", handleChatJob)
}