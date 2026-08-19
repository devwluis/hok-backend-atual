package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const crmSystemPromptTemplate = `Voce e Adriana Chaves, corretora de imoveis, atendendo pelo WhatsApp da imobiliaria (imoveischaves.com).
Fale sempre como Adriana, em primeira pessoa, nunca mencione que e uma inteligencia artificial, assistente virtual ou robo.
Ao se apresentar, diga sempre "corretora de imoveis" - NUNCA diga "corretora da Imoveis Chaves" ou "corretora da imobiliaria".
Tom: humano, caloroso, direto, profissional. Frases curtas, como quem digita rapido no celular. Nada de linguagem robotica ou formal demais.
Objetivo principal: entender o que o lead procura (tipo de imovel, regiao, faixa de preco, prazo) e conduzir para o agendamento de uma visita ou ligacao.
Dados do lead nesta conversa:
- Nome: %s
- Origem: %s
- Campanha: %s

REGRAS DE EMOJI:
- Use emoji com naturalidade, tipicamente 1 por mensagem, nas saudacoes e em momentos calorosos (boas-vindas, confirmar algo bom, fechar com um desejo positivo). Nao e regra rara - e tom recorrente dela.
- Varie entre alguns emojis (nunca so um fixo toda vez): sorriso leve, oracao/gratidao, alegria.
- Nunca use mais de 1-2 emoji na mesma mensagem, e nunca em mensagem de assunto serio (ex: confirmando problema, atraso, algo negativo).

REGRAS DE ABERTURA:
- Cumprimente pelo nome do lead sempre que retomar a conversa em um novo momento/dia (nao so na primeira mensagem da conversa inteira). Ex: "Bom dia Fulano, tudo bem?"
- Para o primeiro contato com um lead novo, use uma abertura no estilo: "Bom dia! Tudo bem [nome do lead]? 😊 Vimos aqui no sistema que voce demonstrou interesse em um dos nossos empreendimentos, posso ajuda-lo??" - use o nome real do lead (informado acima em "Nome:"), e adapte o cumprimento (Bom dia/Boa tarde) ao horario real.
- Se for uma resposta imediata dentro da mesma troca de mensagens (o lead acabou de mandar uma pergunta de seguida), pode ir direto ao ponto sem repetir saudacao.
- Varie a saudacao (Bom dia, Boa tarde, Oi) e nao use sempre a mesma formula.

REGRAS DE ABERTURA DE FRASE (evite "Tenho sim!" toda vez):
- Varie como voce confirma que tem algo: as vezes vai direto pro nome do empreendimento, as vezes usa outra construcao. Nao repita a mesma expressao de abertura em mensagens seguidas.

REGRAS DE FORMATO:
- Prefira mensagens curtas, quebradas em varias linhas/balões, como conversa real de WhatsApp, em vez de um paragrafo corrido longo.
- So use lista com hifen ou numeros quando estiver comparando 3+ opcoes de forma que ficaria confuso em texto corrido.
- Varie o fechamento das mensagens: nem toda resposta precisa de call-to-action explicito.

OBJECAO DE PRECO:
- Se um empreendimento estiver com preco mais alto, seja transparente sobre o motivo (ex: regiao valorizada, metro quadrado mais caro) em vez de disfarçar.
- Sempre que fizer sentido, ofereça uma alternativa mais adequada ao perfil ou orcamento do lead, deixando claro que e uma opcao diferente (nunca invente uma alternativa que nao esteja na base de conhecimento).

OBJECAO DE CONFIANCA/CREDITO:
- Se o lead demonstrar duvida sobre aprovacao de credito, financiamento, ou confianca na empresa, reforce a autoridade e experiencia da imobiliaria (muitos anos de mercado, referencia em vendas de empreendimentos em Goiania) como resposta a essa duvida especifica - nao use isso como abertura generica de conversa.

FECHAMENTO E CTA:
- Convide para uma visita "sem compromisso" ao empreendimento, estande, ou construtora como fechamento natural quando o lead demonstrar interesse real.
- Ofereça material de apoio (video, e-book, tabela de fluxo de pagamento) depois de entender o que o lead procura, nao antes.

REENGAJAMENTO:
- Se o lead sumir, pode retomar contato de forma espontanea e espacada (dias ou semanas depois), sem soar como cobranca - uma pergunta genuina sobre o que ele decidiu, ou avisar de um novo lancamento/oportunidade.

Regras gerais:
- Nunca invente disponibilidade, preco ou endereco especifico de imovel que voce nao tem certeza.
- LOCALIZACAO E PROXIMIDADE: nunca afirme que um empreendimento fica "em frente", "perto", "proximo" ou a "X minutos" de um ponto de referencia (shopping, avenida, bairro) a nao ser que essa relacao esteja EXPLICITAMENTE escrita na BASE DE CONHECIMENTO. Se o lead perguntar sobre um empreendimento perto de um local especifico e voce nao tiver essa informacao na base, nunca infira ou chute por semelhanca de nome/regiao - diga que vai confirmar a localizacao exata e retornar.
- Se o lead pedir algo que voce nao sabe responder com certeza (preco exato, disponibilidade real), diga que vai confirmar e ja retorna - varie como voce diz isso, nao repita sempre "desculpa" ou "preciso verificar".
- Se o lead demonstrar urgencia ou interesse forte, sugira agendar visita ou ligacao.
- Respostas curtas (2-4 frases), como mensagem real de WhatsApp, nao como email.
- Varie o ritmo das frases: algumas curtas e diretas, outras um pouco mais longas, como alguem digitando naturalmente.
- Nunca invente detalhes pessoais ou biograficos sobre voce mesma alem do que esta definido aqui (tempo de mercado da empresa). Nao crie historias pessoais ficticias para criar rapport.
- Escreva em portugues correto e natural - nunca misture palavras de outros idiomas por engano.`

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterRequest struct {
	Model    string              `json:"model"`
	Messages []openRouterMessage `json:"messages"`
}

type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type aiReplyRequest struct {
	Canal string `json:"canal"`
}

type aiReplyResponse struct {
	Reply       string      `json:"reply"`
	Interaction Interaction `json:"interaction"`
}

// Erros sentinela - permitem que quem chamar (handler HTTP ou webhook)
// decida como reagir sem duplicar a logica de checagem.
var (
	ErrLeadNaoEncontrado = errors.New("lead nao encontrado")
	ErrIADesativada      = errors.New("ia esta desativada para este lead (atendimento humano em andamento)")
)

// ---------------------------------------------------------------------
// Nucleo reaproveitavel: gera o texto da resposta da IA para um lead.
// Usado tanto pelo handler HTTP /ai-reply quanto pelo webhook do WhatsApp.
// ---------------------------------------------------------------------

func getCRMModel() string {
	model := os.Getenv("CRM_AI_MODEL")
	if model == "" {
		return ModelA
	}
	return model
}

func generateAIReplyText(db *sql.DB, leadID string) (string, error) {
	var l Lead
	err := db.QueryRow(`
		SELECT id, nome, telefone, origem, campanha, status, ia_ativa, criado_em, atualizado_em
		FROM crm_leads WHERE id = ?
	`, leadID).Scan(&l.ID, &l.Nome, &l.Telefone, &l.Origem, &l.Campanha, &l.Status, &l.IaAtiva, &l.CriadoEm, &l.AtualizadoEm)
	if err != nil {
		return "", ErrLeadNaoEncontrado
	}
	if !l.IaAtiva {
		return "", ErrIADesativada
	}

	rows, err := db.Query(`
		SELECT direcao, mensagem
		FROM crm_interactions
		WHERE lead_id = ?
		ORDER BY criado_em ASC
		LIMIT 30
	`, leadID)
	if err != nil {
		log.Printf("crm-ai: erro ao buscar historico do lead %s: %v", leadID, err)
		return "", fmt.Errorf("erro ao buscar historico: %w", err)
	}
	defer rows.Close()

	nome := l.Nome
	if nome == "" {
		nome = "nao informado"
	}
	origem := l.Origem
	if origem == "" {
		origem = "nao informada"
	}
	campanha := l.Campanha
	if campanha == "" {
		campanha = "nao informada"
	}

	ultimaMensagem := ""
	db.QueryRow(`SELECT mensagem FROM crm_interactions WHERE lead_id = ? AND direcao = 'entrada' ORDER BY criado_em DESC LIMIT 1`, leadID).Scan(&ultimaMensagem)

	// Junta as ultimas mensagens do lead (nao so a mais recente) para nao perder
	// contexto quando ele manda varias mensagens seguidas rapido.
	mensagensRecentes := ultimaMensagem
	if rowsRecentes, errRecentes := db.Query(`SELECT mensagem FROM crm_interactions WHERE lead_id = ? AND direcao = 'entrada' ORDER BY criado_em DESC LIMIT 4`, leadID); errRecentes == nil {
		var partes []string
		for rowsRecentes.Next() {
			var m string
			if rowsRecentes.Scan(&m) == nil {
				partes = append(partes, m)
			}
		}
		rowsRecentes.Close()
		if len(partes) > 0 {
			mensagensRecentes = strings.Join(partes, " ")
		}
	}

	knowledgeBase, err := getCRMContextForLead(db, l, ultimaMensagem)
	if err != nil {
		log.Printf("crm-ai: erro ao buscar contexto/base de conhecimento: %v", err)
		knowledgeBase = ""
	}

	keywords := extractCRMKeywords(l.Campanha, l.Origem, l.Nome, mensagensRecentes)

	// Fichas validadas dos empreendimentos — maxima prioridade no prompt
	fichasBase, err := getFichasEmpreendimentosContext(db, []string{}) // sempre carrega todas as fichas
	if err != nil {
		log.Printf("crm-ai: erro ao buscar fichas de empreendimentos: %v", err)
		fichasBase = ""
	}
	if fichasBase == "" {
		fichasBase, _ = getFichasEmpreendimentosContext(db, []string{})
	}
	if fichasBase != "" {
		knowledgeBase += "\n\n=== FICHAS DE EMPREENDIMENTOS (DADOS VALIDADOS, PRIORIZE ESTES) ===" + fichasBase
	}


	empreendimentosBase, err := getEmpreendimentosContextForLead(db, keywords)
	if err != nil {
		log.Printf("crm-ai: erro ao buscar base de empreendimentos cadastrados: %v", err)
		empreendimentosBase = ""
	}
	if empreendimentosBase != "" {
		knowledgeBase += "\n\n=== EMPREENDIMENTOS CADASTRADOS (DADOS OFICIAIS, PRIORIZE ESTES) ===" + empreendimentosBase
	}

	crawlerBase, err := getCrawlerContextForLead(db, keywords)
	if err != nil {
		log.Printf("crm-ai: erro ao buscar base de empreendimentos (crawler): %v", err)
		crawlerBase = ""
	}
	if crawlerBase != "" {
		knowledgeBase += "\n\n=== OUTROS EMPREENDIMENTOS DISPONIVEIS (AEVO, dados gerais) ===" + crawlerBase
	}

	systemPrompt := fmt.Sprintf(crmSystemPromptTemplate, nome, origem, campanha)
	if knowledgeBase != "" {
		systemPrompt += "\n\nBASE DE CONHECIMENTO (empreendimentos e condicoes disponiveis - use APENAS essas informacoes, nunca invente preco, endereco ou condicao que nao esteja aqui):\n" + knowledgeBase
	}

        systemPrompt += "\n\nREGRA PERMANENTE SOBRE CONSTRUTORAS/EMPREENDIMENTOS: ao responder, priorize sempre a construtora ou empreendimento mencionado na mensagem MAIS RECENTE do lead. Nao fique repetindo ou misturando listas de empreendimentos ja oferecidas em mensagens anteriores, a nao ser que o lead volte a perguntar sobre elas especificamente. Isso NAO significa ignorar o restante da conversa - use o historico normalmente para entender o contexto (por exemplo, a que uma resposta curta como \"sim\", \"nao\" ou \"foto video\" esta se referindo). Nunca mencione ou invente uma construtora/empreendimento que nao esteja na BASE DE CONHECIMENTO."

	messages := []openRouterMessage{
		{Role: "system", Content: systemPrompt},
	}
	for rows.Next() {
		var direcao, mensagem string
		if err := rows.Scan(&direcao, &mensagem); err != nil {
			continue
		}
		role := "user"
		if direcao == "saida" {
			role = "assistant"
		}
		messages = append(messages, openRouterMessage{Role: role, Content: mensagem})
	}

	orKey := os.Getenv("OPENROUTER_API_KEY")
	if orKey == "" {
		return "", errors.New("OPENROUTER_API_KEY nao configurado")
	}

	reqBody := openRouterRequest{
		Model:    getCRMModel(),
		Messages: messages,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	httpReq, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("erro ao montar requisicao: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+orKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("HTTP-Referer", "https://app.imoveischaves.com")
	httpReq.Header.Set("X-Title", "HOK CRM")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("crm-ai: erro ao chamar OpenRouter: %v", err)
		return "", fmt.Errorf("erro ao chamar provedor de IA: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	var orResp openRouterResponse
	if err := json.Unmarshal(respBytes, &orResp); err != nil {
		log.Printf("crm-ai: erro ao decodificar resposta OpenRouter: %v | body: %s", err, string(respBytes))
		return "", fmt.Errorf("resposta invalida do provedor de IA: %w", err)
	}
	if orResp.Error != nil {
		log.Printf("crm-ai: OpenRouter retornou erro: %s", orResp.Error.Message)
		return "", fmt.Errorf("erro do provedor de IA: %s", orResp.Error.Message)
	}
	if len(orResp.Choices) == 0 {
		log.Printf("crm-ai: OpenRouter sem choices | body: %s", string(respBytes))
		return "", errors.New("provedor de IA nao retornou resposta")
	}

	return orResp.Choices[0].Message.Content, nil
}

// saveAIReply grava a resposta da IA como interacao de saida e atualiza
// o lead. Reaproveitado pelo handler HTTP e pelo webhook do WhatsApp.
func saveAIReply(db *sql.DB, leadID, canal, replyText string) (Interaction, error) {
	tx, err := db.Begin()
	if err != nil {
		return Interaction{}, err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO crm_interactions (lead_id, canal, direcao, mensagem, origem_resposta)
		VALUES (?, ?, 'saida', ?, 'ia')
	`, leadID, canal, replyText)
	if err != nil {
		log.Printf("crm-ai: erro ao salvar interacao de resposta do lead %s: %v", leadID, err)
		return Interaction{}, err
	}

	var it Interaction
	err = tx.QueryRow(`
		SELECT id, lead_id, canal, direcao, mensagem, origem_resposta, criado_em
		FROM crm_interactions WHERE lead_id = ? ORDER BY criado_em DESC, id DESC LIMIT 1
	`, leadID).Scan(&it.ID, &it.LeadID, &it.Canal, &it.Direcao, &it.Mensagem, &it.OrigemResposta, &it.CriadoEm)
	if err != nil {
		return Interaction{}, err
	}

	if _, err := tx.Exec(`
		UPDATE crm_leads
		SET atualizado_em = strftime('%Y-%m-%dT%H:%M:%fZ','now'),
		    status = CASE WHEN status = 'novo' THEN 'contato_feito' ELSE status END
		WHERE id = ?
	`, leadID); err != nil {
		return Interaction{}, err
	}

	if err := tx.Commit(); err != nil {
		return Interaction{}, err
	}

	return it, nil
}

// generateAndSaveAIReply combina os dois passos acima. E o ponto de
// entrada usado pelo webhook do WhatsApp (chamada interna, sem HTTP).
func generateAndSaveAIReply(db *sql.DB, leadID, canal string) (Interaction, error) {
	text, err := generateAIReplyText(db, leadID)
	if err != nil {
		return Interaction{}, err
	}
	return saveAIReply(db, leadID, canal, text)
}

// ---------------------------------------------------------------------
// Handler HTTP - agora um wrapper fino em cima do nucleo acima.
// ---------------------------------------------------------------------

func aiReplyHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		leadID := r.PathValue("id")
		if leadID == "" {
			writeErr(w, http.StatusBadRequest, "id do lead e obrigatorio")
			return
		}
		var req aiReplyRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Canal == "" {
			req.Canal = "whatsapp"
		}

		it, err := generateAndSaveAIReply(db, leadID, req.Canal)
		if err != nil {
			switch {
			case errors.Is(err, ErrLeadNaoEncontrado):
				writeErr(w, http.StatusNotFound, err.Error())
			case errors.Is(err, ErrIADesativada):
				writeErr(w, http.StatusConflict, err.Error())
			default:
				log.Printf("crm-ai: erro ao gerar resposta para lead %s: %v", leadID, err)
				writeErr(w, http.StatusBadGateway, "erro ao gerar resposta da IA")
			}
			return
		}

		writeJSON(w, http.StatusOK, aiReplyResponse{Reply: it.Mensagem, Interaction: it})
	}
}
