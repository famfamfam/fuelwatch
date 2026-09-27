import { useState } from "react";
import { useNavigate } from "react-router";
import { useLogin } from "@/api/hooks";
import { errorMessage } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Card, CardContent, Input, Label } from "@/components/ui/primitives";

export function Login() {
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const mut = useLogin();
  const nav = useNavigate();

  return (
    <div className="flex min-h-dvh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardContent className="pt-6">
          <h1 className="mb-1 text-xl font-semibold">⛽ FuelWatch</h1>
          <p className="mb-6 text-sm text-muted-foreground">Вход в панель</p>
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              mut.mutate({ login, password }, { onSuccess: () => nav("/", { replace: true }) });
            }}
          >
            <div className="space-y-2">
              <Label htmlFor="login">Логин</Label>
              <Input id="login" autoComplete="username" autoFocus value={login} onChange={(e) => setLogin(e.target.value)} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">Пароль</Label>
              <Input
                id="password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
            {mut.isError && <p className="text-sm text-sev-critical">{errorMessage(mut.error)}</p>}
            <Button type="submit" className="w-full" disabled={!login || !password || mut.isPending}>
              Войти
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
