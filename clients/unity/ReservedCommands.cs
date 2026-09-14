namespace Knet
{
    /// <summary>
    /// Command IDs reservados pelo protocolo knet.
    /// IDs de usuário devem ser menores que <see cref="CommandError"/>.
    /// Valores espelham o servidor (Go <c>commands.go</c>) e o cliente JS.
    /// </summary>
    public static class ReservedCommands
    {
        /// <summary>Envelope JSON-RPC 2.0 (pedido/resposta).</summary>
        public const uint JsonRpc = 0xFFFFFFFF;

        /// <summary>Envelope de resposta de erro JSON-RPC 2.0.</summary>
        public const uint JsonRpcError = 0xFFFFFFFE;

        /// <summary>ID não reconhecido (servidor → cliente). Reservado por paridade
        /// com o servidor; o servidor atual ignora comandos desconhecidos em silêncio.</summary>
        public const uint InvalidCommand = 0xFFFFFFFD;

        /// <summary>Erro de processamento de comando (servidor → cliente).
        /// Reservado por paridade; o servidor atual não emite este ID.</summary>
        public const uint CommandError = 0xFFFFFFFC;
    }
}
