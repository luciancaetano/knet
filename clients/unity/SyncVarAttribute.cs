namespace Knet
{
    /// <summary>
    /// Primeiro byte do payload de um sync var. Valores casam com o lado servidor
    /// (<c>syncvar.Tag)</c> e com o cliente JS.
    ///
    /// <para>
    /// A payload de um sync var tem o formato <c>[1 byte tag][valor UTF-8]</c>,
    /// onde o valor é a representação em texto do valor
    /// (ex: "42", "3.14", "true") — mesma convenção do <c>strconv</c> no Go.
    /// </para>
    /// </summary>
    public enum SyncVarType : byte
    {
        Int32 = 0x01,
        Int64 = 0x02,
        Float32 = 0x03,
        Float64 = 0x04,
        Bool = 0x05,
        String = 0x06,
    }

    /// <summary>
    /// Marca um campo como estado autoritativo do servidor sincronizado sobre um
    /// comando ID.
    ///
    /// <para>
    /// Para cada campo com um <c>[SyncVar]</c>, o cliente faz bind automático de um
    /// handler de comando que decodifica o payload formatado
    /// <c>[tag][valor UTF-8]</c> e grava o valor decodificado no campo. O valor
    /// deve seguir a convenção de texto de <c>strconv</c> do servidor.
    /// </para>
    ///
    /// <example>
    /// <code>
    /// [SyncVar(0x0010, SyncVarType.Float32)]
    /// private float posX;
    ///
    /// void Start() => client.BindSyncVars(this);
    /// </code>
    /// </example>
    /// </summary>
    [System.AttributeUsage(System.AttributeTargets.Field)]
    public class SyncVarAttribute : System.Attribute
    {
        public readonly uint CommandId;
        public readonly SyncVarType Type;

        public SyncVarAttribute(uint commandId, SyncVarType type)
        {
            CommandId = commandId;
            Type = type;
        }
    }
}
