import { useState } from "react";
import { Copy, Plus } from "lucide-react";
import { toast } from "sonner";
import { useCreateDevice } from "@/api/hooks";
import { errorMessage } from "@/api/client";
import type { PairingCode } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/primitives";
import { Dialog, DialogContent, DialogTrigger } from "@/components/ui/overlay";
import { formatTime } from "@/lib/utils";

export function PairingCodeView({ code }: { code: PairingCode }) {
  const copy = () => {
    navigator.clipboard?.writeText(code.pairing_code).then(() => toast.success("Код скопирован"));
  };
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2 rounded-lg bg-muted px-4 py-3">
        <span className="font-mono text-2xl font-semibold tracking-widest">{code.pairing_code}</span>
        <Button variant="ghost" size="icon" onClick={copy} title="Скопировать">
          <Copy />
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">
        Действует до {formatTime(code.expires_at)}, один раз. На телефоне откройте FuelWatch, введите адрес сервера
        ({window.location.origin}) и этот код.
      </p>
    </div>
  );
}

export function AddDeviceDialog() {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [code, setCode] = useState<PairingCode | null>(null);
  const create = useCreateDevice();

  const reset = (o: boolean) => {
    setOpen(o);
    if (!o) {
      setName("");
      setCode(null);
      create.reset();
    }
  };

  return (
    <Dialog open={open} onOpenChange={reset}>
      <DialogTrigger asChild>
        <Button>
          <Plus /> Добавить устройство
        </Button>
      </DialogTrigger>
      <DialogContent
        title={code ? "Код привязки" : "Новое устройство"}
        description={code ? undefined : "Название видно в панели и в уведомлениях."}
      >
        {code ? (
          <>
            <PairingCodeView code={code} />
            <Button onClick={() => reset(false)}>Готово</Button>
          </>
        ) : (
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              create.mutate(name, {
                onSuccess: (r) => setCode(r),
                onError: (err) => toast.error(errorMessage(err)),
              });
            }}
          >
            <div className="space-y-2">
              <Label htmlFor="dev-name">Название</Label>
              <Input
                id="dev-name"
                autoFocus
                placeholder="АЗС на Ленина"
                value={name}
                maxLength={64}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <Button type="submit" className="w-full" disabled={!name.trim() || create.isPending}>
              Создать и получить код
            </Button>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
