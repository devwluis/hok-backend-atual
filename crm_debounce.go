package main

import (
	"database/sql"
	"log"
	"sync"
	"time"
)

const whatsappDebounceWindow = 7 * time.Second

type debounceEntry struct {
	timer    *time.Timer
	telefone string
}

var (
	debounceMu     sync.Mutex
	debounceTimers = map[string]*debounceEntry{}
)

// scheduleAIReply agenda a geracao e envio da resposta da IA para um lead,
// com debounce: se o lead mandar varias mensagens em sequencia rapida, so
// processamos uma vez, apos o periodo de silencio (whatsappDebounceWindow).
func scheduleAIReply(db *sql.DB, leadID, telefone string) {
	debounceMu.Lock()
	defer debounceMu.Unlock()

	entry := &debounceEntry{telefone: telefone}
	entry.timer = time.AfterFunc(whatsappDebounceWindow, func() {
		processDebouncedReply(db, leadID, entry)
	})
	if existing, ok := debounceTimers[leadID]; ok {
		existing.timer.Stop()
		log.Printf("whatsapp-debounce: lead %s mandou nova mensagem antes do timer estourar - reiniciando janela de %v", leadID, whatsappDebounceWindow)
	}
	debounceTimers[leadID] = entry
}

// processDebouncedReply roda depois que o debounce expira: gera UMA resposta
// da IA considerando todas as mensagens acumuladas do lead nesse periodo
// (generateAndSaveAIReply sempre le o historico mais recente do banco).
// Recebe o entry original como prova de identidade: se o lead mandou outra
// mensagem e um novo *debounceEntry* foi registrado entretanto, este callback
// (do timer antigo, que pode ter disparado mesmo com Stop()) nao pode apagar
// nem responder pelo entry novo — senao a resposta chega fora da janela.
func processDebouncedReply(db *sql.DB, leadID string, mine *debounceEntry) {
	debounceMu.Lock()
	current, ok := debounceTimers[leadID]
	if !ok || current != mine {
		debounceMu.Unlock()
		return
	}
	delete(debounceTimers, leadID)
	debounceMu.Unlock()

	it, err := generateAndSaveAIReply(db, leadID, "whatsapp")
	if err != nil {
		log.Printf("whatsapp-debounce: erro ao gerar resposta da ia para o lead %s: %v", leadID, err)
		return
	}

	if err := sendWhatsAppMessage(mine.telefone, it.Mensagem); err != nil {
		log.Printf("whatsapp-debounce: erro ao enviar resposta da ia para %s: %v", mine.telefone, err)
		return
	}

	log.Printf("whatsapp-debounce: resposta da ia enviada (apos debounce) - lead %s (%s): %s", leadID, mine.telefone, truncate(it.Mensagem, 80))
}
