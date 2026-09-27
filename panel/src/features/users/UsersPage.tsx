import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Plus, Power } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError, errorMessage } from "@/api/client";
import { useMe } from "@/api/hooks";
import { Button } from "@/components/ui/button";
import { Badge, Card, Input, Label, Skeleton } from "@/components/ui/primitives";
import { Dialog, DialogContent } from "@/components/ui/overlay";
import { formatAgo, useNow } from "@/lib/utils";

interface UserRow {
  id: number;
  login: string;
  created_at: string;
  last_login_at: string | null;
  disabled: boolean;
}

const MIN_PASSWORD = 8;

function useUsers() {
  return useQuery({ queryKey: ["users"], queryFn: () => api<{ items: UserRow[] }>("/users").then((r) => r.items) });
}

/** Форма логина/пароля: и для нового пользователя, и для смены пароля (без логина). */
function UserDialog({
  open,
  onClose,
  user,
}: {
  open: boolean;
  onClose: () => void;
  user?: UserRow; // есть — меняем пароль; нет — создаём
}) {
  const qc = useQueryClient();
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const save = useMutation({
    mutationFn: () =>
      user ? api(`/users/${user.id}`, { method: "PATCH", body: { password } }) : api("/users", { method: "POST", body: { login, password } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["users"] });
      toast.success(user ? `Пароль ${user.login} изменён, его другие сессии завершены` : `Пользователь ${login} добавлен`);
      close();
    },
    onError: (e) => (e instanceof ApiError && e.fields ? setErrors(e.fields) : toast.error(errorMessage(e))),
  });
  const close = () => {
    setLogin("");
    setPassword("");
    setErrors({});
    onClose();
  };
  const short = password.length > 0 && password.length < MIN_PASSWORD;
  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()}>
      <DialogContent title={user ? `Новый пароль для ${user.login}` : "Новый пользователь"}>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          {!user && (
            <div className="space-y-2">
              <Label htmlFor="u-login">Логин</Label>
              <Input id="u-login" autoFocus autoComplete="off" value={login} maxLength={64} onChange={(e) => setLogin(e.target.value)} />
              {errors.login && <p className="text-xs text-sev-critical">{errors.login}</p>}
            </div>
          )}
          <div className="space-y-2">
            <Label htmlFor="u-pass">Пароль</Label>
            <Input
              id="u-pass"
              type="password"
              autoFocus={!!user}
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            <p className={short ? "text-xs text-sev-critical" : "text-xs text-muted-foreground"}>
              {errors.password ?? `Не короче ${MIN_PASSWORD} символов`}
            </p>
          </div>
          <Button type="submit" className="w-full" disabled={(!user && !login.trim()) || password.length < MIN_PASSWORD || save.isPending}>
            {user ? "Сменить пароль" : "Добавить"}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** Пользователи (docs/06-panel.md §12): список, добавить, сменить пароль, отключить. Себя отключить нельзя. */
export function UsersPage() {
  const users = useUsers();
  const me = useMe();
  const qc = useQueryClient();
  const now = useNow(60_000);
  const [adding, setAdding] = useState(false);
  const [pwFor, setPwFor] = useState<UserRow | undefined>();
  const toggle = useMutation({
    mutationFn: (u: UserRow) => api(`/users/${u.id}`, { method: "PATCH", body: { disabled: !u.disabled } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
    onError: (e) => toast.error(errorMessage(e)),
  });

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-xl font-semibold">Пользователи</h1>
        <Button onClick={() => setAdding(true)}>
          <Plus /> Добавить
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">Все пользователи панели равноправны. Действия с последствиями записываются в журналы с логином.</p>
      {users.isPending ? (
        <Skeleton className="h-40" />
      ) : (
        <Card className="divide-y">
          {users.data?.map((u) => {
            const self = u.id === me.data?.id;
            return (
              <div key={u.id} className="flex flex-wrap items-center gap-3 px-3 py-2">
                <div className="min-w-0 flex-1">
                  <p className="flex items-center gap-2 text-sm font-medium">
                    {u.login}
                    {self && <Badge tone="info">это вы</Badge>}
                    {u.disabled && <Badge tone="critical">отключён</Badge>}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    вход: {u.last_login_at ? formatAgo(u.last_login_at, now) : "не входил"}
                  </p>
                </div>
                <Button size="sm" variant="ghost" onClick={() => setPwFor(u)}>
                  <KeyRound /> Пароль
                </Button>
                {!self && (
                  <Button
                    size="sm"
                    variant="ghost"
                    className={u.disabled ? undefined : "text-sev-critical"}
                    disabled={toggle.isPending}
                    onClick={() => (u.disabled || window.confirm(`Отключить ${u.login}? Его сессии завершатся.`)) && toggle.mutate(u)}
                  >
                    <Power /> {u.disabled ? "Включить" : "Отключить"}
                  </Button>
                )}
              </div>
            );
          })}
        </Card>
      )}
      <UserDialog open={adding} onClose={() => setAdding(false)} />
      <UserDialog open={!!pwFor} user={pwFor} onClose={() => setPwFor(undefined)} />
    </div>
  );
}
