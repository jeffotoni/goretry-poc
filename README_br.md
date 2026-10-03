# POC de Retry HTTP em Memoria

[![Go 1.27.1](https://img.shields.io/badge/Go-1.27.1-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![POC](https://img.shields.io/badge/status-POC-f0ad4e)](#poc-de-retry-http-em-memoria)
[![quick](https://img.shields.io/badge/servidor-quick-0A7E8C)](https://github.com/jeffotoni/quick)
[![req](https://img.shields.io/badge/cliente%20http-req-0A7E8C)](https://github.com/jeffotoni/req)
[![log](https://img.shields.io/badge/logging-log-0A7E8C)](https://github.com/jeffotoni/log)

Esta e a versao em portugues. A documentacao padrao em ingles esta em [README.md](README.md).

Esta POC demonstra um servidor HTTP em Go que recebe um payload JSON e o encaminha para um segundo servico. Quando o destino apresenta uma falha temporaria, a requisicao entra em uma fila limitada em memoria e e processada novamente por um worker pool.

## Bibliotecas usadas

Esta POC usa tres bibliotecas pequenas e focadas do [github.com/jeffotoni](https://github.com/jeffotoni):

| Biblioteca | Uso nesta POC |
| --- | --- |
| [`quick`](https://github.com/jeffotoni/quick) | Servidor HTTP e roteamento. Mantem a configuracao da API concisa usando a pilha HTTP padrao do Go. |
| [`req`](https://github.com/jeffotoni/req) | Cliente HTTP usado para enviar o payload JSON ao servico destino. O mesmo cliente e reutilizado entre as tentativas. |
| [`log`](https://github.com/jeffotoni/log) | Logs estruturados da aplicacao, com saida JSON ou text e campos pesquisaveis como request ID, tentativa, worker e status. |

A politica de retry e implementada por esta POC em `internal/retry`. O retry automatico do `req` permanece desativado para existir apenas um coordenador de retry, com uma fila, uma politica de backoff e uma janela de retry.

O objetivo e didatico: mostrar uma estrategia simples, controlada e observavel de retry sem adicionar uma fila persistente.

## Visao rapida

```text
+------------------+       +---------------------+       +------------------+
| Cliente          |       | Servidor quick      |       | Servico destino |
| POST /dispatch   | ----> | tentativa inicial  | ----> | recebe JSON      |
+------------------+       +----------+----------+       +--------+---------+
                                      |                           |
                         sucesso <---+                           |
                                      |                           |
                         erro temporario                          |
                                      v                           |
                           +----------------+                      |
                           | Fila limitada  |                      |
                           +--------+-------+                      |
                                    v                              |
                           +----------------+                      |
                           | Worker pool   | ---------------------+
                           +--------+-------+       retry          |
                                    v                              |
                           backoff + janela                       |
                                                                   |
                         sucesso ou falha final <-----------------+
```

Diagrama complementar: [`docs/retry-flow.svg`](docs/retry-flow.svg).

## Como funciona

1. O handler `POST /dispatch` recebe qualquer JSON valido.
2. O payload e enviado imediatamente ao destino usando um cliente `req` compartilhado.
3. Se o destino responder com sucesso, a resposta e devolvida ao cliente.
4. Se ocorrer uma falha temporaria, a requisicao e adicionada a fila limitada.
5. O handler retorna `202 Accepted` e nao mantem a conexao aberta durante toda a janela.
6. Um worker aguarda o backoff e executa a proxima tentativa.
7. O ciclo continua ate sucesso, erro permanente, fila cheia ou expiracao da janela.

O retry externo e a politica principal desta POC. O retry interno do `req` permanece desativado para evitar duas camadas independentes de tentativas.

## Falhas que entram em retry

- Erros de transporte, como `EOF`, timeout, conexao recusada e conexao resetada.
- HTTP `408 Request Timeout`.
- HTTP `429 Too Many Requests`.
- HTTP `500 Internal Server Error`.
- HTTP `502 Bad Gateway`.
- HTTP `503 Service Unavailable`.
- HTTP `504 Gateway Timeout`.

Os demais erros HTTP, especialmente os `4xx`, sao tratados como permanentes e nao entram na fila.

O header `Retry-After` e respeitado quando o destino responder com um valor valido. O mock desta POC consegue gerar esse header por meio do `X-Mock-Retry-After`.

## Backoff e janela

O backoff inicial padrao e:

```text
tentativa 1: 500 ms
tentativa 2: 1 s
tentativa 3: 2 s
tentativa 4: 4 s
...
```

O atraso possui um limite maximo e pode usar jitter para evitar que varias requisicoes sejam reenviadas ao mesmo tempo.

A janela padrao e de tres minutos. Ao expirar, a requisicao e descartada e o evento e registrado como `retry window expired`. Nao existe tentativa infinita.

### Tempos e modelo de execucao

Com a configuracao padrao, uma requisicao que recebe um `502` retryable segue estes tempos:

```text
chamada inicial: imediatamente -> 502
retry #1:        apos aproximadamente 500ms
retry #2:        apos aproximadamente 1s
retry #3:        apos aproximadamente 2s
retry #4:        apos aproximadamente 4s
retry #5:        apos aproximadamente 8s
retry #6:        apos aproximadamente 16s
proximas:        ate 30s entre tentativas
limite total:    janela de retry de 3 minutos
```

O jitter esta ativo por padrao, portanto os atrasos reais variam aproximadamente assim:

```text
500ms -> 400-600ms
1s    -> 800-1200ms
2s    -> 1.6-2.4s
```

Com `POC_RETRY_JITTER=false`, os atrasos sao exatos.

O fluxo de execucao e:

```text
resposta inicial 502
  -> requisicao entra na fila
  -> handler responde 202 Accepted
  -> worker aguarda o backoff
  -> worker executa o retry
```

Cada requisicao possui seu proprio ciclo de retry. Este mecanismo nao e um circuit breaker e o sistema nao abre um estado global de bloqueio. A protecao vem de:

- Fila limitada.
- Worker pool fixo.
- Janela maxima de tres minutos.
- Backoff crescente.
- Atraso maximo de 30 segundos.
- Nenhuma goroutine nova por tentativa.

## Componentes

```text
cmd/retry-poc/       servidor de dispatch
cmd/mock-target/     servico destino para os testes
internal/retry/      fila, workers, backoff e janela
internal/httpapi/    handlers HTTP
internal/config/     nomes e composicao das variaveis da POC
internal/pkg/env/    leitura e conversao das variaveis de ambiente
docs/retry-flow.svg  diagrama do fluxo
```

## Configuracao

As variaveis desta POC usam o prefixo `POC_` e nao reutilizam os nomes da implementacao original.

| Variavel | Default | Funcao |
| --- | --- | --- |
| `POC_SERVER_ADDR` | `:8080` | Endereco do servidor de dispatch |
| `POC_DESTINATION_URL` | `http://127.0.0.1:8081/target` | URL do servico destino |
| `POC_MOCK_ADDR` | `:8081` | Endereco do servidor mock |
| `POC_RETRY_ACTIVE` | `true` | Ativa ou desativa a fila de retry |
| `POC_RETRY_WINDOW` | `3m` | Tempo maximo de vida da requisicao |
| `POC_RETRY_DELAY_BASE` | `500ms` | Backoff da primeira tentativa |
| `POC_RETRY_DELAY_MAX` | `30s` | Limite do backoff |
| `POC_RETRY_POOL_SIZE` | `4` | Quantidade fixa de workers |
| `POC_RETRY_BUFFER_SIZE` | `256` | Capacidade da fila em memoria |
| `POC_RETRY_TIMEOUT` | `30s` | Timeout de cada chamada HTTP |
| `POC_RETRY_JITTER` | `true` | Adiciona variacao aleatoria ao backoff |
| `POC_LOG_FORMAT` | `json` | Formato do log: `json` ou `text` |

Duracoes aceitam o formato nativo do Go, por exemplo `500ms`, `2s` e `3m`.

### Formato dos logs

O formato padrao e `json`, porque preserva os campos estruturados e facilita a consulta por ferramentas de observabilidade. Para leitura manual durante o desenvolvimento, use:

```bash
POC_LOG_FORMAT=text go run ./cmd/retry-poc
```

JSON:

```json
{"time":"2026-10-03T19:30:00Z","level":"INFO","msg":"retry attempt","service":"retry-poc","worker":2,"request_id":"poc-retry-001","attempt":2}
```

Text:

```text
2026-10-03T19:30:00Z | INFO | - | retry attempt | service=retry-poc | worker=2 | request_id=poc-retry-001 | attempt=2
```

O retry recebe uma interface pequena de logging e nao depende diretamente do formato escolhido. A implementacao usa `github.com/jeffotoni/log` por baixo, mantendo os eventos estruturados nos dois formatos.

## Pre-requisitos

- Go `1.24` ou superior.
- Acesso aos repositorios publicos `quick`, `req` e `log` para o download dos modulos.

Baixar e organizar as dependencias:

```bash
go mod download
go mod tidy
```

## Subir localmente

Abra dois terminais na raiz desta POC.

### Terminal 1: servico mock

```bash
go run ./cmd/mock-target
```

Saida esperada:

```text
time=... level=INFO msg="mock target started" address=[::]:8081
```

### Terminal 2: servidor de retry

```bash
POC_RETRY_DELAY_BASE=100ms \
POC_RETRY_DELAY_MAX=2s \
POC_RETRY_JITTER=false \
POC_LOG_FORMAT=json \
go run ./cmd/retry-poc
```

Saida esperada:

```text
time=... level=INFO msg="retry poc started" address=[::]:8080 target=http://127.0.0.1:8081/target
```

Os valores acima reduzem os atrasos para os testes. Se nenhuma variavel for definida, os defaults da tabela serao usados.

## Testes com curl

### 1. Sucesso imediato

O mock recebe o payload e responde `200`.

```bash
curl -i -X POST http://127.0.0.1:8080/dispatch \
  -H 'Content-Type: application/json' \
  -H 'X-Request-ID: poc-success-001' \
  -H 'X-Mock-Status: 200' \
  -d '{"message":"success"}'
```

Resultado esperado: `200 OK`.

### 2. Erro permanente

O status `400` nao entra em retry.

```bash
curl -i -X POST http://127.0.0.1:8080/dispatch \
  -H 'Content-Type: application/json' \
  -H 'X-Request-ID: poc-4xx-001' \
  -H 'X-Mock-Status: 400' \
  -d '{"message":"permanent error"}'
```

Resultado esperado: `400` e somente uma chamada no mock.

### 3. Falha temporaria seguida de sucesso

O header `X-Mock-Status-Sequence` define o status de cada tentativa. A posicao e identificada pelo sufixo do header `X-Retry-POC-Attempt-ID`.

```bash
curl -i -X POST http://127.0.0.1:8080/dispatch \
  -H 'Content-Type: application/json' \
  -H 'X-Request-ID: poc-retry-001' \
  -H 'X-Mock-Status-Sequence: 502,502,200' \
  -d '{"message":"retry me"}'
```

Comportamento esperado:

```text
tentativa 1 -> 502 -> entra na fila -> resposta inicial 202
tentativa 2 -> 502 -> aguarda novo backoff
tentativa 3 -> 200 -> retry succeeded
```

O mock deve imprimir o mesmo payload nas tres tentativas.

### 4. Todos os status retryable

```bash
curl -i -X POST http://127.0.0.1:8080/dispatch \
  -H 'Content-Type: application/json' \
  -H 'X-Request-ID: poc-statuses-001' \
  -H 'X-Mock-Status-Sequence: 408,429,500,502,503,504,200' \
  -d '{"message":"status sequence"}'
```

### 5. Retry-After

O mock devolve `Retry-After: 1` quando recebe `X-Mock-Retry-After: 1`.

```bash
curl -i -X POST http://127.0.0.1:8080/dispatch \
  -H 'Content-Type: application/json' \
  -H 'X-Request-ID: poc-retry-after-001' \
  -H 'X-Mock-Status-Sequence: 429,200' \
  -H 'X-Mock-Retry-After: 1' \
  -d '{"message":"retry after"}'
```

A segunda tentativa deve ocorrer aproximadamente um segundo depois da primeira.

### 6. Erro de transporte

Pare o mock ou aponte o dispatch para uma porta sem servidor:

```bash
POC_DESTINATION_URL=http://127.0.0.1:8999/target \
POC_RETRY_DELAY_BASE=100ms \
POC_RETRY_WINDOW=3s \
go run ./cmd/retry-poc
```

Erros como `connection refused`, `EOF` e timeout entram no mesmo fluxo de retry.

### 7. Consultar saude e estatisticas

```bash
curl -s http://127.0.0.1:8080/health
curl -s http://127.0.0.1:8080/stats
```

Exemplo de estatisticas:

```json
{
  "queued": 1,
  "running": 0,
  "retries": 2,
  "succeeded": 1,
  "exhausted": 0,
  "queue_full": 0,
  "non_retryable": 0
}
```

## Regras de memoria e concorrencia

- A fila tem capacidade fixa.
- O numero de workers e fixo.
- Nao existe uma goroutine nova por tentativa.
- Cada worker reutiliza o cliente HTTP compartilhado.
- O body e copiado para que possa ser reenviado.
- A requisicao expira quando a janela chega ao fim.
- Se a fila estiver cheia, o servidor informa `retry_queue_full`.

## Idempotencia

Um timeout nao prova que o destino nao processou o request. Todas as tentativas reutilizam o mesmo `X-Request-ID` e o mesmo body.

Em um sistema real, o destino deve suportar uma chave idempotente ou uma chave natural do recurso. Esta POC nao garante entrega exatamente uma vez.

## Vantagens e desvantagens

| Avaliacao | Item |
| --- | --- |
| 🟢 | Nao bloqueia o cliente durante toda a janela de retry |
| 🟢 | Limita fila, workers e memoria |
| 🟢 | Reutiliza conexoes por meio de um cliente HTTP compartilhado |
| 🟢 | Trata falhas de transporte e status HTTP temporarios |
| 🟢 | Permite testar sequencias de erro por header |
| 🟢 | Mantem o mesmo payload e identificador em todas as tentativas |
| 🔴 | Requisicoes pendentes sao perdidas se o processo cair |
| 🔴 | Nao existe garantia de entrega exatamente uma vez |
| 🔴 | Uma indisponibilidade prolongada pode encher a fila |
| 🔴 | O destino precisa aceitar reprocessamento idempotente |
| 🔴 | A POC nao substitui uma fila persistente |

## Testes automatizados

```bash
go test ./...
go test -race ./...
go vet ./...
```

Os testes validam retry de status temporario e confirmam que erros permanentes nao sao reenviados.

## Como contribuir

Contribuicoes sao bem-vindas. O projeto e intencionalmente pequeno, portanto as alteracoes devem permanecer focadas e faceis de validar.

Antes de abrir um pull request:

- Abra uma issue primeiro ao propor uma mudanca de comportamento ou uma nova funcionalidade.
- Crie uma branch focada a partir da branch default atual.
- Mantenha a politica de retry, os limites da fila e o encerramento cobertos por testes.
- Execute `gofmt` nos arquivos Go alterados.
- Execute `go test ./...`, `go test -race ./...` e `go vet ./...`.
- Atualize o README e o diagrama SVG quando o comportamento ou a arquitetura mudarem.
- Descreva o problema, a solucao e as validacoes realizadas no pull request.

Evite adicionar persistencia, goroutines sem limite ou uma segunda mecanica independente de retry sem discutir antes os trade-offs. Esta POC demonstra intencionalmente retry limitado em memoria.

## Encerramento

Ao receber `SIGINT` ou `SIGTERM`, o servidor para de aceitar novas requisicoes, encerra os workers e libera a fila em memoria. Como nao existe persistencia, tarefas pendentes podem ser perdidas durante o encerramento.
