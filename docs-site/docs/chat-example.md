# Chat Example (JS)

Tutorial completo, passo a passo, para construir um chat multiusuário com `room` (Go) no servidor e um cliente HTML/JavaScript puro. O código deste tutorial é real e roda — está em [`examples/chat/`](https://github.com/luciancaetano/knet/tree/main/examples/chat) no repositório.

## 1. Visão geral

Vamos construir:

- um servidor Go que agrupa todos os clientes numa `room` chamada `"lobby"`
- broadcast de mensagens de chat para todos na sala
- notificações automáticas de quem entra e quem sai
- reconexão automática do lado do cliente
- a peça mais importante: distinguir quando alguém **saiu de propósito** (clicou em "Sair") de quando **caiu sem querer** (rede caiu, aba travou, timeout)

### Protocolo

| ID | Direção | Payload | Propósito |
|---|---|---|---|
| `0x0001` SetName | client → server | texto (nome desejado) | define/atualiza o nome ao conectar ou reconectar |
| `0x0002` Chat | client → server → sala | JSON `{"from","text"}` | mensagem de chat |
| `0x0003` UserJoined | server → sala | JSON `{"name"}` | alguém entrou |
| `0x0004` UserLeft | server → sala | JSON `{"name"}` | alguém saiu **voluntariamente** |
| `0x0005` UserDisconnected | server → sala | JSON `{"name"}` | alguém caiu **sem querer** |

## 2. Servidor Go passo a passo

O servidor é organizado em 4 arquivos, cada um com uma responsabilidade — não é um único `main.go` monolítico:

```
examples/chat/
├── protocol.go   # command IDs e tipos de payload (JSON)
├── names.go      # armazenamento de nome por clientID
├── server.go     # chatServer: OnConnect/OnDisconnect e handlers
└── main.go       # bootstrap: monta o ws.Server e registra tudo
```

### 2.1 `protocol.go` — Command IDs e payloads

Definimos os IDs do protocolo do chat como constantes, e os formatos JSON usados nos payloads. IDs de aplicação precisam ficar abaixo de `0xFFFFFFFC` — essa faixa é reservada pelo knet (veja [Reference](reference.md#reserved-command-ids)).

```go
--8<-- "examples/chat/protocol.go:commands"
```

### 2.2 `names.go` — Guardando o nome de cada cliente

O `knet.Client` não guarda estado de aplicação — só identidade de conexão (`ID()`, `RemoteAddr()`, etc). Então o servidor mantém seu próprio mapa `clientID -> nome`, isolado num tipo dedicado (`nameStore`) e protegido por mutex, já que handlers rodam em goroutines concorrentes.

```go
--8<-- "examples/chat/names.go:names"
```

### 2.3 `server.go` — o tipo `chatServer`

Toda a lógica do chat vive em métodos de um único tipo, `chatServer`, que guarda a `room` e o `nameStore`. Isso evita variáveis soltas capturadas por closures espalhadas — cada handler é só um método com acesso explícito ao estado que precisa.

```go
--8<-- "examples/chat/server.go:server-type"
```

Uma única `room.Room` chamada `"lobby"` já dá tudo que precisamos: `Add`, `Remove`, `Broadcast` — sem laços manuais sobre clientes, o `Broadcast` da room já é a forma otimizada de enviar para todo mundo.

### 2.4 `OnConnect` e `OnDisconnect` — o coração do exemplo

```go
--8<-- "examples/chat/server.go:connect"
```

`OnConnect` recebe o `knet.Client` assim que a conexão WebSocket é aceita, e retorna `bool`: `true` aceita a conexão, `false` rejeita (fecha com "policy violation"). Aqui só adicionamos o cliente à sala — ele ainda não tem nome, que chega em seguida via `SetName`.

`OnDisconnect` é onde a mágica acontece: `func(client knet.Client, voluntary bool)`. O parâmetro `voluntary` já vem calculado pelo knet:

- `voluntary == true` → o cliente fechou a conexão normalmente (mandou um close frame real — por exemplo, o usuário clicou em "Sair" e o JS chamou `disconnect()`)
- `voluntary == false` → a conexão caiu sem um close limpo (queda de rede, aba fechada à força, timeout de leitura)

Não precisamos de nenhuma mensagem de aplicação tipo `"estou saindo"` — o servidor já sabe a diferença nativamente, e usamos isso direto para escolher entre `UserLeft` e `UserDisconnected`.

### 2.5 Handler `SetName`

```go
--8<-- "examples/chat/server.go:setname-handler"
```

O `UserJoined` só é emitido aqui, depois que o cliente manda um nome — no `OnConnect` ainda não temos como identificá-lo na UI.

### 2.6 Handler `Chat`

```go
--8<-- "examples/chat/server.go:chat-handler"
```

Simples: pega o nome do remetente, embrulha em JSON com o texto, e faz broadcast pra sala inteira — incluindo o próprio remetente (assim o remetente também vê sua mensagem aparecer pela mesma via que os outros).

### 2.7 Servindo `index.html` — e por que `wss://`

Um navegador que carrega a página por `https://` só tem permissão de abrir sockets `wss://` (não `ws://`) — é a mesma regra de "conteúdo misto" que se aplica a `<img>`/`fetch`. Em vez de ensinar um setup que quebra assim que você sobe pra produção atrás de TLS, este exemplo já roda com TLS desde o início, usando um certificado autoassinado de desenvolvimento.

`main.go` sobe um segundo `http.Server` (`serveStatic`), só pra servir `index.html` e o mascote, numa porta separada da porta do WebSocket:

```go
--8<-- "examples/chat/main.go:static-server"
```

### 2.8 `main.go` — montando o servidor

`main.go` fica enxuto: cria o `chatServer`, monta a config do `ws.Server` com `ws.WithTLS` (habilita `wss://`) apontando `OnConnect`/`OnDisconnect` para os métodos de `chatServer`, registra os dois handlers de comando, sobe o servidor estático, e sobe com graceful shutdown.

```go
--8<-- "examples/chat/main.go:bootstrap"
```

O certificado (`cert.pem`/`key.pem`) é gerado automaticamente por `make chat-example` — veja a seção [Rodando o exemplo](#6-rodando-o-exemplo).

## 3. Cliente Web passo a passo

O cliente é um único `index.html`, sem build step. O pacote `@knet/client` ainda não está publicado num CDN, então este exemplo implementa um wrapper vanilla mínimo — o que também serve para mostrar como o protocolo é simples.

### 3.1 O wire format

```js
--8<-- "examples/chat/index.html:wire-format"
```

Cada frame é `[1 byte de versão][4 bytes big-endian de commandID][payload]` — o mesmo formato usado pelos clientes JS e Unity oficiais (veja [Reference](reference.md#wire-format)).

### 3.2 Command IDs no cliente

```js
--8<-- "examples/chat/index.html:commands"
```

### 3.3 O wrapper de WebSocket

```js
--8<-- "examples/chat/index.html:client"
```

Pontos importantes:

- `connect()` abre o socket e agenda reconexão automática em `onclose`, a menos que a desconexão tenha sido manual
- `disconnect()` marca `manuallyDisconnected = true` **antes** de fechar — isso desliga o auto-reconnect e garante que o browser manda um close frame normal, o que faz o servidor ver `voluntary = true`
- o backoff de reconexão é `delay = 1s × min(tentativa, 5)`, o mesmo esquema documentado para os clientes oficiais

### 3.4 Ligando tudo na UI

```js
--8<-- "examples/chat/index.html:ui"
```

## 4. Reconexão na prática

Quando a conexão cai e reconecta automaticamente, o servidor atribui um **novo `clientID`** ao handshake — ele não tem memória de que esse é "o mesmo usuário" de antes. Por isso o evento `connected` sempre reenvia `SetName`, tanto na conexão inicial quanto em toda reconexão: sem isso, o servidor teria um cliente sem nome registrado, e as próximas mensagens de chat apareceriam com o próprio ID como remetente.

## 5. Saída voluntária vs. involuntária

Esse é o requisito mais delicado do chat, e o knet resolve pra você:

| Ação do usuário | O que o servidor vê | Evento emitido |
|---|---|---|
| Clica em "Sair" | Close frame WebSocket normal | `UserLeft` (`voluntary = true`) |
| Fecha a aba do navegador | Geralmente um close frame também é enviado pelo browser | `UserLeft` na maioria dos casos |
| Perde conexão de rede (Wi-Fi cai, processo travado, cabo desconectado) | Nenhum close frame chega; o servidor detecta via timeout de leitura | `UserDisconnected` (`voluntary = false`) |
| Processo do cliente é morto (`kill -9`, crash) | Nenhum close frame | `UserDisconnected` (`voluntary = false`) |

Não há nenhum código de aplicação decidindo isso — é o parâmetro `voluntary` do `OnDisconnect` (seção [2.4](#24-onconnect-e-ondisconnect-o-coracao-do-exemplo)) que carrega essa informação, calculada pelo próprio protocolo WebSocket (recebimento ou não de um close frame válido).

## 6. Rodando o exemplo

```bash
make chat-example
```

Isso primeiro gera um certificado autoassinado de desenvolvimento (`examples/chat/cert.pem` + `key.pem`, via `openssl`, se ainda não existir — veja o target `chat-example-certs` no `Makefile`) e depois sobe o servidor:

- WebSocket: `wss://localhost:8080/ws`
- Página do chat: `https://localhost:8081`

(Equivalente manual, sem o Makefile: `cd examples/chat && go run .` — mas os `.pem` precisam existir antes.)

Como o certificado é autoassinado, o navegador vai alertar de "conexão não seguridade" na primeira visita a cada uma das duas origens (`:8080` e `:8081`) — é esperado em desenvolvimento; aceite o aviso em ambas. Em produção, prefira terminar TLS num proxy reverso (nginx, Caddy) com um certificado real, como descrito em `ws.WithTLS`.

Abra `https://localhost:8081` em duas abas:

1. Em cada aba, digite um nome diferente e clique em **Conectar**
2. Troque mensagens — elas aparecem nas duas abas
3. Numa aba, clique em **Sair** → a outra aba mostra `"<nome> saiu"`
4. Na outra aba, apenas feche a aba do navegador ou desligue a rede → depois do timeout de leitura, você verá `"<nome> caiu (conexão perdida)"` (pode levar alguns segundos, dependendo do `readDeadline` configurado no servidor)

## 7. Próximos passos

- [Rooms & Observer](rooms-observer.md) — mais de uma sala, interest management
- [JavaScript Client](js-client.md) — cliente oficial com reconexão, JSON-RPC e mais, em vez do wrapper vanilla deste exemplo
- [Server API](server-api.md) — configuração completa do servidor, rate limiting, segurança
