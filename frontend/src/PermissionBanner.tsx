import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { useT } from "./i18n";
import { Desktop } from "./mygo";
import { errorText, useNotificationPermission } from "./store";

/** Whether the system keeps the app's notifications from showing; banners turned off may be the user's choice. */
export function usePermissionProblem(withQuiet: boolean): boolean {
  const permission = useNotificationPermission();
  return permission === "denied" || permission === "pending" || (withQuiet && permission === "quiet");
}

/** Says when the system keeps the app's notifications from showing, with a way to its settings. */
export function PermissionBanner({ withQuiet, onError }: { withQuiet: boolean; onError(msg: string): void }) {
  const t = useT();
  const permission = useNotificationPermission();
  if (!usePermissionProblem(withQuiet)) return null;
  const [title, description] =
    permission === "denied"
      ? [t.permissionDenied, t.permissionDeniedText]
      : permission === "pending"
        ? [t.permissionPending, t.permissionPendingText]
        : [t.permissionQuiet, t.permissionQuietText];
  return (
    <Banner
      status={permission === "denied" ? "warning" : "info"}
      title={title}
      description={description}
      endContent={
        <Button
          label={t.openSystemSettings}
          size="sm"
          onClick={() => void Desktop.openNotificationSettings().catch((err) => onError(errorText(err)))}
        />
      }
    />
  );
}
