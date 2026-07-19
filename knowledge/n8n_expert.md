# HOK — Especialista N8N (base de conhecimento)
Atualizado em: 12/07/2026

## Regras gerais de geração de workflow
- SEMPRE usar credentials nativas do n8n (ex: Telegram, HTTP com auth) em vez de
  `$env` dentro de Code nodes. O n8n Code node roda em sandbox que BLOQUEIA
  acesso a `$env` por padrão (N8N_BLOCK_ENV_ACCESS_IN_NODE). Sintoma: erro
  silencioso ou variável undefined dentro do Code node.
- Se precisar de valor de ambiente dentro da lógica, prefira:
  1. Node HTTP Request com header configurado via credential, OU
  2. Node Set anterior que injeta o valor via expression fixa (não via $env)

## Tipos de trigger e como testar
- Webhook Trigger: tem URL de teste em `/webhook-test/{webhookId}` (não o path
  customizado, é o webhookId gerado pelo n8n). Testável via POST direto.
- Manual Trigger: não tem endpoint REST público de "run now" documentado.
  Alternativa real: `docker exec <container_n8n> n8n execute --id=<workflowID>`
  via CLI dentro do container.
- Schedule/Cron Trigger: sem "run now" nativo via API. Para testar, duplicar
  o workflow com um Manual Trigger temporário, ou usar o CLI acima.

## Estrutura de workflow (JSON)
- Todo node precisa de `name` e `type` únicos.
- `connections` referencia nodes pelo `name` (não pelo `id`) — se o nome não
  bater exatamente, a conexão fica órfã e o node não roda.
- Estrutura de connections: `{ "NomeOrigem": { "main": [ [ {"node": "NomeDestino", "type": "main", "index": 0} ] ] } }`

## Erros conhecidos / já resolvidos neste projeto
- Skill routing sempre escolhendo o workflow errado: causado por seção
  `## Execucao` truncando metadados de desambiguação antes da hora. Fix:
  renomear para `## Acao` e reordenar seções do markdown da skill.
- N8N em Docker precisa acessar o backend Go via `172.17.0.1:8082` (gateway
  do bridge network), não via `localhost`.
- Zombie process / porta presa: processo antigo do n8n ou do backend
  sobrevive a restart e segura a porta. Sempre conferir com `lsof -i :PORTA`
  antes de assumir que o restart funcionou.

## Casos de uso já mapeados (templates a expandir)
- Webhook → CRM: Webhook Trigger → HTTP Request (POST /crm/leads) →
  resposta simples de confirmação.
- Schedule → Sync (Google Sheets → contexto IA): Schedule Trigger →
  Google Sheets (Read Rows) → Code/Function (formata texto) →
  HTTP Request (PUT /crm/context).

## Nota de convenção Go (armadilha de build)
- Arquivo terminado em `_test.go` é tratado pelo Go como arquivo de teste
  e é EXCLUÍDO do `go build` normal — só entra em `go test`. Um handler
  definido nesse arquivo (ex: `handleAutomationTest`) fica com erro
  "undefined" mesmo que o conteúdo esteja correto e completo.
  Sintoma: build falha com "undefined: nomeDaFuncao" mesmo após recriar
  o arquivo várias vezes com o conteúdo certo.
  Fix: nunca nomear arquivo de handler/lógica de produção terminando em
  `_test.go`. Usar sufixo `_handlers.go`, `_routes.go`, etc.

## Padrões de tratamento de erro
- Node de erro dedicado (Error Trigger workflow): captura falhas de
  QUALQUER workflow que o referencie em "Settings → Error Workflow".
  Útil para centralizar notificação de falha (ex: manda pro Telegram
  de alerta em vez de falhar silenciosamente).
- Retry automático: em nodes HTTP Request / muitos nodes de integração
  existe aba "Settings → Retry On Fail" com número de tentativas e
  espera entre elas. Preferir isso a implementar retry manual em Code.
- "Continue On Fail" (por node): permite que o workflow continue mesmo
  se aquele node específico falhar — o item de erro vai para a saída
  com campo especial de erro. Útil quando um item ruim não deve travar
  o lote inteiro (ex: processar 50 leads e um tem telefone inválido).

## Processamento em lote
- Split In Batches (Loop Over Items): processa uma lista em blocos de N,
  evita estourar rate limit de API externa. Sempre usar quando a lista
  de entrada tem tamanho variável/desconhecido (ex: leads do Google
  Sheets, resultados de paginação).
- Wait node entre iterações de loop: necessário quando a API externa tem
  rate limit por segundo (ex: Meta Graph API, WhatsApp Cloud API) —
  Split In Batches sozinho não dá espaçamento de tempo, só de volume.

## Paginação em HTTP Request node
- Nem toda API usa o mesmo padrão (offset/limit, cursor, next_page_url).
  O HTTP Request node tem suporte nativo a paginação via aba "Pagination"
  — mas só funciona bem quando a API expõe o total ou um "next" claro no
  corpo/header da resposta. Se a API for não-padrão, geralmente é mais
  simples implementar o loop manual com Code node + IF checando se ainda
  há próxima página, em vez de forçar o recurso nativo.

## Credenciais vs valores fixos
- Toda integração de terceiro (Telegram, WhatsApp Cloud API, Meta Graph
  API, Google Sheets) deve usar Credential nativa do n8n, nunca token
  colado direto em node HTTP Request como header fixo — quebra rotação
  de chave e fica exposto no JSON exportado do workflow (risco se o
  workflow for versionado/commitado, como já aconteceu no hokclaw).
- Credencial "HTTP Header Auth" genérica serve pra qualquer API que só
  precise de um header fixo tipo Authorization/X-API-Key, quando não
  existe node nativo pra aquele serviço.

## Sub-workflows (Execute Workflow node)
- Útil pra reaproveitar lógica comum entre automações diferentes (ex:
  "notificar Telegram" ou "validar payload de lead" chamado por vários
  workflows). Evita duplicar os mesmos nodes em cada automação.
- Dado trafega via input/output explícito do sub-workflow — não há
  acesso automático a variáveis do workflow pai, precisa passar tudo
  como parâmetro de entrada.

## Webhook — segurança básica
- Webhook Trigger sem validação aceita qualquer POST de qualquer origem.
  Mínimo recomendado: checar um header secreto (ex: X-Hok-Token custom)
  logo no primeiro node (IF), ou usar a opção de autenticação nativa do
  Webhook node (Header Auth / Basic Auth) antes de processar o payload.
- Para webhooks do Meta (Lead Ads, WhatsApp), sempre validar a assinatura
  (X-Hub-Signature-256) quando disponível, não só confiar no path secreto
  da URL.

## Expressões e acesso a dados entre nodes
- `{{$json["campo"]}}` acessa o item atual do node anterior direto.
- `{{$node["NomeDoNode"].json["campo"]}}` acessa saída de um node
  específico mais atrás na cadeia (não precisa ser o imediatamente
  anterior).
- `{{$items("NomeDoNode")}}` retorna TODOS os itens daquele node (array),
  útil quando o node atual precisa agregar/comparar múltiplos itens de
  um ponto anterior do fluxo, não só o item corrente.
- Cuidado com merge de branches (node Merge): o item N da branch A não
  necessariamente corresponde ao item N da branch B se os arrays tiverem
  tamanhos diferentes — checar modo do Merge (Append vs Combine vs
  Choose Branch) antes de assumir pareamento por índice.

## Formatação de dados (Function/Code retornando array de items)
- Todo node de código no n8n espera retornar um ARRAY de objetos no
  formato `[{ json: {...} }, ...]`, não um objeto solto. Retornar um
  objeto puro (`return { foo: 1 }`) quebra a cadeia — o próximo node não
  recebe nada ou recebe formato inesperado.
- Para transformar 1 item de entrada em N itens de saída (fan-out),
  retornar array com N elementos `{ json: {...} }` a partir de um Code
  node — não precisa de node especial pra isso.

## Integração com IA dentro de workflow (HTTP Request → LLM)
- Chamada a APIs tipo OpenRouter/Groq/OpenAI via HTTP Request node:
  sempre configurar timeout maior que o default (modelos grandes podem
  passar de 30s) em "Settings → Timeout" do node, senão o node falha por
  timeout mesmo com a API respondendo corretamente, só mais devagar.
  Aplica ao caso do /crm/ai-reply (chamada MiniMax) se algum dia for
  espelhado direto num workflow n8n em vez de ficar só no Go.
- Resposta de LLM em JSON "quase válido" (ex: markdown fences ```json,
  texto antes/depois do JSON): tratar no Code node com regex/strip antes
  de `JSON.parse`, nunca assumir que a resposta crua já é JSON parseável.
- Rate limit de provedor gratuito (ex: Groq free tier) é por token por
  minuto, não só por request — um prompt grande sozinho pode estourar o
  limite mesmo sendo a única chamada do minuto. Já visto no projeto:
  Groq free tier com teto de 6000 TPM.

## Debug e observabilidade
- "Pin Data" em um node: trava a saída daquele node com um valor fixo
  pra poder testar os nodes seguintes sem re-executar toda a cadeia
  (ex: sem precisar mandar webhook de teste de novo toda vez). Ótimo pra
  iterar rápido num node no meio do workflow.
- Histórico de execuções (aba "Executions"): mostra o payload exato que
  entrou/saiu de cada node numa execução passada — primeiro lugar a
  olhar antes de tentar reproduzir um bug manualmente via curl.
- Nome de node com espaço ou caractere especial pode quebrar expressão
  `{{$node["Nome Com Espaço"].json...}}` em algumas versões — preferir
  nomes de node em snake_case ou CamelCase sem espaço quando o node vai
  ser referenciado por nome em expressão de outro node.

## Ativação/atualização de workflow via API (n8n REST)
- PATCH em `/api/v1/workflows/{id}` pra mudar `active: true/false` exige
  que o workflow já esteja com estrutura válida salva — não dá pra
  ativar um workflow com node desconectado ou erro de validação, a API
  retorna erro em vez de ativar mesmo assim.
- Atualizar o JSON completo do workflow via API (PUT) sobrescreve
  TUDO, incluindo posição visual dos nodes — se o objetivo é só mudar
  um parâmetro específico, preferir buscar o JSON atual primeiro (GET),
  alterar só o campo necessário, e reenviar o objeto completo de volta,
  nunca montar um JSON novo do zero.

## Dados binários (imagens, áudio, arquivos)
- Binary data (imagem/áudio/PDF) trafega separado do `json` normal —
  fica em `item.binary`, não em `item.json`. Node que recebe mídia de
  webhook (ex: foto do WhatsApp) precisa acessar via
  `{{$binary.data}}` ou referenciar o binary key certo, não tentar ler
  como se fosse campo json comum.
- Ao repassar binary entre nodes (ex: baixar arquivo → subir em outro
  lugar), confirmar que o node de destino tem opção "Binary Data"
  habilitada nos parâmetros — bytes brutos, se tratados como texto/JSON,
  corrompem no meio do caminho (comum em áudio/imagem do WhatsApp).

## WhatsApp Cloud API oficial (relevante pro imoveischaves)
- Webhook do WhatsApp Cloud API exige verificação inicial (challenge/
  verify_token) num GET antes de aceitar POSTs — separado da lógica de
  receber mensagem. n8n Webhook Trigger precisa responder o `hub.challenge`
  como texto puro no GET de verificação, não como JSON.
- Mensagens recebidas chegam num payload aninhado profundo
  (`entry[0].changes[0].value.messages[0]`) — sempre um Code node logo
  após o Webhook pra "achatar" isso antes do resto do fluxo, evita
  expressões gigantes repetidas em vários nodes.
- Envio de mensagem via Cloud API tem janela de 24h de atendimento
  gratuito por contato — fora dessa janela, só templates pré-aprovados
  pela Meta funcionam, mensagem livre é rejeitada pela API. Relevante
  pro fluxo de IA respondendo lead: checar timestamp da última mensagem
  do usuário antes de tentar responder livremente.

## Node Merge — modos e quando usar cada um
- **Append**: concatena todas as listas em uma só, sem combinar por
  índice. Uso: juntar leads de duas fontes diferentes (Meta Ads + manual).
- **Combine (by position/index)**: junta item N da branch A com item N
  da branch B. Só funciona bem se ambas branches vierem do MESMO
  conjunto original e na MESMA ordem — arriscado se uma branch filtrou
  itens (IF) e a outra não.
- **Combine (by matching fields)**: junta por valor de campo em comum
  (ex: telefone), é o mais seguro pra correlacionar dados de fontes
  diferentes sem depender de ordem/índice.

## Versionamento e nodes "v1 vs v2"
- Vários nodes nativos do n8n tiveram mudanças de versão (Set, IF,
  Switch) com parâmetros/comportamento diferentes entre v1 e v2 —
  workflow exportado de uma versão antiga do n8n pode não importar
  corretamente numa instância mais nova, ou os campos aparecerem vazios
  no editor mesmo com o JSON tecnicamente válido. Ao importar workflow
  antigo (ex: do Hetzner pro Hostinger), abrir e conferir visualmente
  cada node antes de assumir que migrou 100%.

## Timezone e datas
- n8n usa a timezone configurada em `GENERIC_TIMEZONE` (env var da
  instância) para Schedule Trigger e funções de data — se o servidor
  está em UTC mas o negócio opera em horário de Brasília, agendamento
  tipo "todo dia às 9h" vai disparar na hora errada se essa env var não
  estiver setada corretamente.
- `{{$now}}` e `{{$today}}` retornam no timezone da instância, não do
  navegador de quem está editando o workflow — cuidado ao testar local
  vs produção se as timezones das máquinas forem diferentes.

## Community nodes e nodes customizados
- Node de comunidade (não-oficial) instalado via npm no container n8n
  precisa ser reinstalado manualmente após rebuild/recriação do
  container, se não estiver persistido em volume — perda silenciosa
  comum quando se recria o container do zero (ex: troca de servidor
  como Hetzner → Hostinger) sem migrar a pasta de nodes customizados.
