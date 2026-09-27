import { useState } from "react";
import { useNavigate } from "react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Pencil, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError, errorMessage } from "@/api/client";
import { qk, usePatchDevice } from "@/api/hooks";
import type { DeviceSummary } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/primitives";
import { Dialog, DialogContent } from "@/components/ui/overlay";

/** Переименовать устройство (имя видно в панели, уведомлениях и Telegram). */
export function RenameDevice({ d }: { d: DeviceSummary }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(d.name);
  const [error, setError] = useState("");
  const patch = usePatchDevice(d.id);
  return (
    <>
      <Button variant="ghost" size="icon" className="size-8" title="Переименовать" onClick={() => (setName(d.name), setError(""), setOpen(true))}>
        <Pencil />
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent title="Переименовать устройство">
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              patch.mutate(
                { name },
                {
                  onSuccess: () => setOpen(false),
                  onError: (err) => (err instanceof ApiError && err.fields?.name ? setError(err.fields.name) : toast.error(errorMessage(err))),
                },
              );
            }}
          >
            <div className="space-y-2">
              <Label htmlFor="dev-rename">Название</Label>
              <Input id="dev-rename" autoFocus value={name} maxLength={64} onChange={(e) => setName(e.target.value)} />
              {error && <p className="text-xs text-sev-critical">{error}</p>}
            </div>
            <Button type="submit" className="w-full" disabled={!name.trim() || name.trim() === d.name || patch.isPending}>
              Сохранить
            </Button>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}

/**
 * Удалить устройство навсегда: кадры, визиты, журнал. Необратимо, поэтому нужно ввести название.
 * Если телефон ещё стоит на объекте, он перестанет подключаться и покажет экран привязки.
 */
export function DeleteDevice({ d }: { d: DeviceSummary }) {
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState("");
  const qc = useQueryClient();
  const nav = useNavigate();
  const del = useMutation({
    mutationFn: () => api(`/devices/${d.id}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.setQueryData<DeviceSummary[]>(qk.devices, (list) => list?.filter((x) => x.id !== d.id));
      qc.removeQueries({ queryKey: qk.device(d.id) });
      toast.success(`«${d.name}» удалено`);
      nav("/", { replace: true });
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  return (
    <>
      <Button variant="ghost" className="w-full text-sev-critical" onClick={() => (setTyped(""), setOpen(true))}>
        <Trash2 /> Удалить устройство
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent title={`Удалить «${d.name}»?`}>
          <div className="space-y-3 text-sm">
            <p>Удалятся все кадры, визиты, зоны, настройки и журнал этого устройства. Вернуть их нельзя.</p>
            {d.online && (
              <p className="rounded-md bg-sev-warning/15 px-3 py-2">
                Телефон сейчас на связи. Он перестанет присылать данные и покажет экран привязки. Если нужно только
                приостановить — используйте «Отключить устройство».
              </p>
            )}
            <div className="space-y-2">
              <Label htmlFor="dev-del">
                Введите название <b>{d.name}</b> для подтверждения
              </Label>
              <Input id="dev-del" autoFocus autoComplete="off" value={typed} onChange={(e) => setTyped(e.target.value)} />
            </div>
            <Button variant="destructive" className="w-full" disabled={typed.trim() !== d.name || del.isPending} onClick={() => del.mutate()}>
              <Trash2 /> Удалить навсегда
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
