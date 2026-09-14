# Introdução

## O que é o knet?

**knet** é uma biblioteca Go para construir servidores de jogos e aplicações em tempo real sobre WebSocket. Em vez de reinventar a comunicação binária, gerenciamento de conexões e broadcasting toda vez que você começa um projeto multiplayer, o knet entrega isso pronto — e sai do seu caminho para o resto.

No núcleo, a ideia é simples: mensagens trafegam como `4 bytes de command ID + payload binário`. Sem parsing de JSON no caminho quente, sem reflection, sem camadas por cima da camada. Você registra um handler para um comando, o knet decodifica sem cópia e entrega ao seu código. Quando você precisa de request/response (login, queries, config), o JSON-RPC 2.0 está disponível como opção — não como obrigação.

## Por que essa biblioteca existe

Construir a parte de rede de um jogo multiplayer do zero significa resolver, de novo, os mesmos problemas: como enviar dados eficientemente, como agrupar jogadores em salas, como sincronizar estado sem desperdiçar banda, como manter um tick rate estável, como não deixar um cliente malicioso derrubar o servidor. Esses são problemas de infraestrutura, não de gameplay — e resolver mal qualquer um deles custa caro depois, em bugs de produção às 3 da manhã.

O knet nasceu para isolar exatamente essa camada:

- **Protocolo binário** com command pattern, para você gastar bytes só com o que importa
- **`room`** para agrupar clientes (lobbies, partidas, zonas, canais de chat)
- **`observer`** para interest management — cada cliente só recebe atualizações do que realmente lhe interessa
- **`syncvar`** para replicar estado só quando ele muda, evitando broadcast de dados repetidos
- **`timing`** para rodar um game loop em tick rate fixo, desacoplado da frequência da sua simulação

Cada uma dessas peças é opcional. Você pode usar só o servidor base e nada mais.

## Performance e escalabilidade

O knet é construído em cima de duas decisões deliberadas:

**Protocolo binário, decodificação zero-copy.** O payload que chega no seu handler é a mesma fatia de memória recebida do socket — sem alocar uma cópia, sem serializar/desserializar JSON no caminho mais quente do servidor (o processamento de mensagens de jogo, que roda centenas ou milhares de vezes por segundo por cliente conectado).

**Concorrência nativa do Go.** Cada handler roda na sua própria goroutine — o modelo de concorrência do Go permite lidar com dezenas de milhares de conexões simultâneas sem o overhead de threads do SO. Rate limiting por cliente (token bucket), timeouts de leitura/escrita e limite de payload (10 MB por padrão) protegem o servidor contra clientes lentos, enfileiramento sem controle e ataques triviais de payload gigante — sem que você precise escrever nada disso.

Na prática, isso significa: um único processo Go, rodando em uma máquina modesta, aguenta uma carga de clientes concorrentes que exigiria uma frota inteira em linguagens com runtime mais pesado ou modelos de concorrência baseados em threads do SO.

## Baixo custo

Go compila para um binário único, estático, sem VM e sem runtime pesado para carregar. Isso se traduz direto em custo de infraestrutura:

- **Menos máquinas para a mesma carga** — o modelo de goroutines do Go processa muito mais conexões concorrentes por núcleo de CPU do que a maioria das alternativas com threads do SO
- **Menos memória por conexão** — uma goroutine custa poucos KB de stack inicial, não MBs de thread
- **Deploy simples e barato** — um binário estático, sem dependências de runtime, cabe numa imagem Docker minúscula (veja o `Dockerfile` do projeto) e sobe em qualquer lugar
- **Sem taxa de licença ou runtime proprietário** — Go e o knet são open source, MIT license

O resultado prático: você paga por CPU e memória de verdade, não por overhead de plataforma. Para um jogo multiplayer que precisa escalar de um punhado de jogadores a milhares sem reescrever a stack de rede, esse é o tipo de economia que se acumula mês a mês.

## Para quem é o knet

Se você está construindo qualquer coisa que precise de:

- Estado compartilhado entre múltiplos clientes em tempo real (jogos multiplayer, salas colaborativas, dashboards ao vivo)
- Um protocolo eficiente que não desperdice banda com overhead de texto
- Uma base que escale sem exigir reescrita de arquitetura quando o número de usuários crescer

... o knet foi feito para você. Ele não tenta ser um motor de jogo, nem impõe arquitetura de ECS ou ticks fixos por padrão — é a camada de rede, e só ela, feita para ser rápida, previsível e barata de operar.

## Próximo passo

Pronto para ver o código? Siga para [Getting Started](getting-started.md) e suba seu primeiro servidor em poucos minutos.
