using System;

namespace Knet
{
    /// <summary>
    /// Payload shapes for the RoomManager join/leave/event protocol. Mirrors
    /// <c>roommanager/protocol.go</c> — field names match the JSON exactly
    /// (required by <c>JsonUtility</c>, which has no rename attribute).
    /// </summary>
    [Serializable]
    public class RoomJoinRequest
    {
        public string roomId;
        /// Name of a handler registered server-side via Manager.Define. Empty for legacy flat rooms.
        public string roomType;
    }

    [Serializable]
    public class RoomLeaveRequest
    {
        public string roomId;
    }

    [Serializable]
    public class RoomJoinAck
    {
        public string roomId;
        public string[] members;
    }

    [Serializable]
    public class RoomLeaveAck
    {
        public string roomId;
    }

    [Serializable]
    public class RoomError
    {
        public string roomId;
        public string code;
        public string message;
    }

    [Serializable]
    public class RoomMemberEvent
    {
        public string roomId;
        public string clientId;
        public string type;
    }

    [Serializable]
    public class RoomResumeSyncEntry
    {
        public string roomId;
        public string[] members;
    }

    [Serializable]
    public class RoomResumeSync
    {
        public RoomResumeSyncEntry[] rooms;
    }

    [Serializable]
    public class RoomMessageRequest
    {
        public string roomId;
        public string type;
        public string data;
    }

    [Serializable]
    public class RoomMessageEvent
    {
        public string roomId;
        public string senderId;
        public string type;
        public string data;
    }
}
