// AdminUserRouting — /admin/user-routing
//
// PKG-4 / architecture §19. Admin sees all greentokey users with a NewAPI
// binding, can filter by group, and can flip a user's NewAPI group via a
// modal — which the backend turns into a NewAPI PUT /api/user/ + a local
// gtk_newapi_binding.newapi_group update.
//
// Layout mirrors the existing /admin/users page (Card + admin-card class
// for the page chrome, Table primitives for the body). Auth is upstream
// at the route level (AdminRequired wrapper in router.tsx) so this
// component assumes the viewer is already an admin.

import { useEffect, useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
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
import { Input } from "@/components/ui/input.tsx";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select.tsx";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog.tsx";
import { Badge } from "@/components/ui/badge.tsx";
import { RotateCw, Pencil, Loader2 } from "lucide-react";
import {
  listUserRouting,
  updateUserRouting,
  SUGGESTED_GROUPS,
  type UserRoutingRow,
} from "@/api/userRouting.ts";

const PAGE_SIZE = 50;

function UserRouting() {
  const { t } = useTranslation();

  const [rows, setRows] = useState<UserRoutingRow[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [groupFilter, setGroupFilter] = useState<string>(""); // "" = all
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [editing, setEditing] = useState<UserRoutingRow | null>(null);
  const [editGroup, setEditGroup] = useState("");
  const [saving, setSaving] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const resp = await listUserRouting({
        limit: PAGE_SIZE,
        offset,
        group: groupFilter || undefined,
      });
      setRows(resp.users);
      setTotal(resp.total);
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setLoading(false);
    }
  }, [offset, groupFilter]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const openEdit = (row: UserRoutingRow) => {
    setEditing(row);
    setEditGroup(row.newapi_group);
  };

  const closeEdit = () => {
    if (saving) return;
    setEditing(null);
    setEditGroup("");
  };

  const submitEdit = async () => {
    if (!editing) return;
    const next = editGroup.trim();
    if (!next) {
      toast.error(t("admin.user-routing.empty-group-error"));
      return;
    }
    setSaving(true);
    try {
      await updateUserRouting(editing.coai_user_id, next);
      toast.success(
        t("admin.user-routing.update-success", {
          username: editing.username || `#${editing.coai_user_id}`,
          group: next,
        }),
      );
      setEditing(null);
      setEditGroup("");
      await refresh();
    } catch (e: any) {
      toast.error(e?.message || String(e));
    } finally {
      setSaving(false);
    }
  };

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1;

  return (
    <div className={`user-routing`}>
      <Card className={`admin-card`}>
        <CardHeader className={`select-none`}>
          <CardTitle className={`flex items-center justify-between`}>
            <span>{t("admin.user-routing.title")}</span>
            <Button
              variant="ghost"
              size="sm"
              onClick={refresh}
              disabled={loading}
              aria-label={t("admin.user-routing.refresh")}
            >
              <RotateCw
                className={loading ? "h-4 w-4 animate-spin" : "h-4 w-4"}
              />
            </Button>
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col gap-3">
            <div className="flex flex-row items-center gap-2 flex-wrap">
              <span className="text-sm text-muted-foreground">
                {t("admin.user-routing.filter-group")}
              </span>
              <Select
                value={groupFilter || "__all__"}
                onValueChange={(v) => {
                  setGroupFilter(v === "__all__" ? "" : v);
                  setOffset(0);
                }}
              >
                <SelectTrigger className="w-[200px]">
                  <SelectValue
                    placeholder={t("admin.user-routing.all-groups")}
                  />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__all__">
                    {t("admin.user-routing.all-groups")}
                  </SelectItem>
                  {SUGGESTED_GROUPS.map((g) => (
                    <SelectItem key={g} value={g}>
                      {g}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <span className="text-sm text-muted-foreground ml-auto">
                {t("admin.user-routing.total", { total })}
              </span>
            </div>

            {error && (
              <div className="text-sm text-destructive">
                {t("admin.user-routing.load-error", { error })}
              </div>
            )}

            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>
                    {t("admin.user-routing.col-coai-id")}
                  </TableHead>
                  <TableHead>
                    {t("admin.user-routing.col-username")}
                  </TableHead>
                  <TableHead>
                    {t("admin.user-routing.col-newapi-id")}
                  </TableHead>
                  <TableHead>{t("admin.user-routing.col-group")}</TableHead>
                  <TableHead>
                    {t("admin.user-routing.col-quota")}
                  </TableHead>
                  <TableHead className="text-right">
                    {t("admin.user-routing.col-actions")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.length === 0 && !loading && (
                  <TableRow>
                    <TableCell
                      colSpan={6}
                      className="text-center text-muted-foreground"
                    >
                      {t("admin.user-routing.empty")}
                    </TableCell>
                  </TableRow>
                )}
                {rows.map((row) => (
                  <TableRow key={row.coai_user_id}>
                    <TableCell>{row.coai_user_id}</TableCell>
                    <TableCell>{row.username || "—"}</TableCell>
                    <TableCell>{row.newapi_user_id}</TableCell>
                    <TableCell>
                      <Badge variant="secondary">{row.newapi_group}</Badge>
                    </TableCell>
                    <TableCell>
                      {row.last_known_quota.toLocaleString()}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => openEdit(row)}
                      >
                        <Pencil className="h-4 w-4 mr-1" />
                        {t("admin.user-routing.edit")}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>

            <div className="flex flex-row items-center justify-between text-sm text-muted-foreground">
              <span>
                {t("admin.user-routing.page-of", {
                  page: currentPage,
                  total: totalPages,
                })}
              </span>
              <div className="flex flex-row gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={offset === 0 || loading}
                  onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                >
                  {t("admin.user-routing.prev")}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={offset + PAGE_SIZE >= total || loading}
                  onClick={() => setOffset(offset + PAGE_SIZE)}
                >
                  {t("admin.user-routing.next")}
                </Button>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      <Dialog
        open={editing != null}
        onOpenChange={(open) => {
          if (!open) closeEdit();
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t("admin.user-routing.edit-title", {
                username:
                  editing?.username || `#${editing?.coai_user_id ?? ""}`,
              })}
            </DialogTitle>
            <DialogDescription>
              {t("admin.user-routing.edit-desc")}
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-3">
            <div className="text-sm text-muted-foreground">
              {t("admin.user-routing.current-group")}:{" "}
              <Badge variant="secondary">{editing?.newapi_group}</Badge>
            </div>
            <Select value={editGroup} onValueChange={setEditGroup}>
              <SelectTrigger>
                <SelectValue
                  placeholder={t("admin.user-routing.pick-group")}
                />
              </SelectTrigger>
              <SelectContent>
                {SUGGESTED_GROUPS.map((g) => (
                  <SelectItem key={g} value={g}>
                    {g}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              value={editGroup}
              onChange={(e) => setEditGroup(e.target.value)}
              placeholder={t("admin.user-routing.custom-group-placeholder")}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={closeEdit} disabled={saving}>
              {t("admin.user-routing.cancel")}
            </Button>
            <Button onClick={submitEdit} disabled={saving}>
              {saving && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              {t("admin.user-routing.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export default UserRouting;
