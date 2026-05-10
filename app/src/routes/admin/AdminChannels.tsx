// AdminChannels — /admin/channels-routing (greentokey NewAPI inventory view)
//
// PKG-4 / architecture §19. Read-only view of every NewAPI channel with
// its group whitelist, model list, sub2API badge, status, and latency.
// Companion to /admin/user-routing — once you've decided which group a
// user is in, this page tells you which channels (and therefore which
// upstream providers) that group can hit.
//
// v0 is read-only: the founder edits channel groups via the raw NewAPI
// admin dashboard for now (the backend PUT endpoint is a 501 stub —
// see newapi/admin_routing.go header for the deferral rationale).
//
// Naming + path notes:
//   - File is AdminChannels.tsx (rather than Channels.tsx) to disambiguate
//     from the sibling Channel.tsx which mounts CoAI-upstream
//     ChannelSettings against CoAI's own internal channel CRUD.
//   - Route path is /admin/channels-routing (NOT /admin/channels) for the
//     same reason: /admin/channel (singular) is the CoAI page; we add a
//     greentokey-specific path so both can coexist.

import { useEffect, useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card.tsx";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table.tsx";
import { Button } from "@/components/ui/button.tsx";
import { Badge } from "@/components/ui/badge.tsx";
import { RotateCw } from "lucide-react";
import {
  listAdminChannels,
  type ChannelRow,
} from "@/api/userRouting.ts";

function AdminChannels() {
  const { t } = useTranslation();
  const [channels, setChannels] = useState<ChannelRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const resp = await listAdminChannels();
      setChannels(resp.channels);
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  return (
    <div className={`admin-channels`}>
      <Card className={`admin-card`}>
        <CardHeader className={`select-none`}>
          <CardTitle className={`flex items-center justify-between`}>
            <span>{t("admin.channels.title")}</span>
            <Button
              variant="ghost"
              size="sm"
              onClick={refresh}
              disabled={loading}
              aria-label={t("admin.channels.refresh")}
            >
              <RotateCw
                className={loading ? "h-4 w-4 animate-spin" : "h-4 w-4"}
              />
            </Button>
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col gap-3">
            <div className="text-sm text-muted-foreground">
              {t("admin.channels.read-only-note")}
            </div>

            {error && (
              <div className="text-sm text-destructive">
                {t("admin.channels.load-error", { error })}
              </div>
            )}

            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("admin.channels.col-id")}</TableHead>
                  <TableHead>{t("admin.channels.col-name")}</TableHead>
                  <TableHead>{t("admin.channels.col-provider")}</TableHead>
                  <TableHead>{t("admin.channels.col-models")}</TableHead>
                  <TableHead>{t("admin.channels.col-group")}</TableHead>
                  <TableHead>{t("admin.channels.col-status")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {channels.length === 0 && !loading && (
                  <TableRow>
                    <TableCell
                      colSpan={6}
                      className="text-center text-muted-foreground"
                    >
                      {t("admin.channels.empty")}
                    </TableCell>
                  </TableRow>
                )}
                {channels.map((ch) => (
                  <TableRow key={ch.id}>
                    <TableCell>{ch.id}</TableCell>
                    <TableCell>
                      <div className="flex flex-col gap-1">
                        <span>{ch.name}</span>
                        {ch.is_sub2api && (
                          <Badge variant="destructive" className="w-fit">
                            sub2API
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-col gap-1">
                        <span className="text-sm">
                          {ch.provider_label || ch.provider}
                        </span>
                        <span className="text-xs text-muted-foreground">
                          type={ch.type}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-row flex-wrap gap-1 max-w-md">
                        {(ch.models || []).map((m) => (
                          <Badge
                            key={m}
                            variant="outline"
                            className="text-xs"
                          >
                            {m}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-row flex-wrap gap-1">
                        {ch.group
                          .split(",")
                          .map((g) => g.trim())
                          .filter(Boolean)
                          .map((g) => (
                            <Badge key={g} variant="secondary">
                              {g}
                            </Badge>
                          ))}
                      </div>
                    </TableCell>
                    <TableCell>
                      {ch.status === 1 ? (
                        <Badge variant="default">
                          {t("admin.channels.status-enabled")}
                        </Badge>
                      ) : (
                        <Badge variant="outline">
                          {t("admin.channels.status-disabled")}
                        </Badge>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

export default AdminChannels;
