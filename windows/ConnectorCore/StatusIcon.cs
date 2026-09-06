namespace ConnectorCore;

public static class StatusIcon
{
    public static string Describe(BackendState state) => state switch
    {
        BackendState.Starting => "時計",
        BackendState.Running => "チェック",
        BackendState.BackendDown => "警告",
        BackendState.BackendDownPermanent => "停止",
        BackendState.BackendIncompatible => "互換性なし",
        _ => "終了",
    };
}
