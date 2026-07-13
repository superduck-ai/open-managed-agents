import { useNavigate } from "@tanstack/react-router";
import { ArrowLeft, CircleAlert, Loader2, MailCheck } from "lucide-react";
import { type FormEvent, type ReactNode, useEffect, useMemo, useState } from "react";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button, ButtonLink } from "@/shared/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/shared/ui/card";
import { Field, FieldDescription, FieldLabel } from "@/shared/ui/field";
import { Input } from "@/shared/ui/input";
import { sendMagicLink, verifyMagicLink } from "../../shared/auth/api";
import { useAuth } from "../../shared/auth/context";
import { returnToFromSearch } from "../../shared/auth/redirects";
import { useI18n } from "../../shared/i18n";

type LoginStep = "email" | "code";

type LoginFlowProps = {
  initialEmail?: string;
  onSendMagicLink?: (emailAddress: string) => Promise<unknown>;
  onVerifyMagicLink?: (emailAddress: string, code: string) => Promise<unknown>;
  onAuthenticated: () => Promise<void> | void;
};

const topNavItems = [
  { id: "auth.login.topNav.developerDocs", label: "Developer Docs", href: "https://docs.anthropic.com/" },
  { id: "auth.login.topNav.apiReference", label: "API Reference", href: "https://docs.anthropic.com/" },
  { id: "auth.login.topNav.cookbooks", label: "Cookbooks", href: "https://docs.anthropic.com/" },
  { id: "auth.login.topNav.quickstarts", label: "Quickstarts", href: "https://docs.anthropic.com/" },
] as const;

export function LoginPage() {
  const { status, refresh } = useAuth();
  const navigate = useNavigate();
  const returnTo = useMemo(() => returnToFromSearch(window.location.search), []);
  const { msg } = useI18n();

  useEffect(() => {
    if (status === "authenticated") {
      void navigate({ href: returnTo, replace: true });
    }
  }, [navigate, returnTo, status]);

  if (status === "loading" || status === "authenticated") {
    return (
      <LoginShell>
        <section className="flex min-h-[calc(100vh-9rem)] items-center justify-center py-10">
          <Card className="w-full max-w-md">
            <CardContent className="py-8 text-sm text-muted-foreground">
              {msg("app.loading", "Loading Open Managed Agents...")}
            </CardContent>
          </Card>
        </section>
      </LoginShell>
    );
  }

  return (
    <LoginFlow
      onAuthenticated={async () => {
        await refresh();
        await navigate({ href: returnTo, replace: true });
      }}
    />
  );
}

export function LoginFlow({
  initialEmail = "",
  onSendMagicLink = sendMagicLink,
  onVerifyMagicLink = verifyMagicLink,
  onAuthenticated,
}: LoginFlowProps) {
  const { msg } = useI18n();
  const [step, setStep] = useState<LoginStep>("email");
  const [email, setEmail] = useState(initialEmail);
  const [submittedEmail, setSubmittedEmail] = useState(initialEmail);
  const [code, setCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isResending, setIsResending] = useState(false);

  async function handleEmailSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const normalizedEmail = email.trim();
    setError(null);
    setNotice(null);

    if (!isValidEmail(normalizedEmail)) {
      setError(msg("auth.login.invalidEmail", "Enter a valid email address."));
      return;
    }

    setIsSubmitting(true);
    try {
      await onSendMagicLink(normalizedEmail);
      setSubmittedEmail(normalizedEmail);
      setStep("code");
      setNotice(msg("auth.login.codeNotice", "Enter the 6-digit code sent to your email."));
    } catch (err) {
      setError(errorMessage(err, msg("auth.login.sendCodeFailed", "Could not send a code. Try again.")));
    } finally {
      setIsSubmitting(false);
    }
  }

  async function handleCodeSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setNotice(null);

    if (code.length !== 6) {
      setError(msg("auth.login.codeLengthError", "Enter the 6-digit verification code."));
      return;
    }

    setIsSubmitting(true);
    try {
      await onVerifyMagicLink(submittedEmail, code);
      await onAuthenticated();
    } catch (err) {
      setError(errorMessage(err, msg("auth.login.verifyCodeFailed", "Could not verify that code. Try again.")));
    } finally {
      setIsSubmitting(false);
    }
  }

  async function handleResend() {
    setError(null);
    setNotice(null);
    setIsResending(true);
    try {
      await onSendMagicLink(submittedEmail);
      setNotice(msg("auth.login.codeResent", "A new code was sent."));
    } catch (err) {
      setError(errorMessage(err, msg("auth.login.resendFailed", "Could not resend the code. Try again.")));
    } finally {
      setIsResending(false);
    }
  }

  return (
    <LoginShell>
      <section className="grid min-h-[calc(100vh-9rem)] gap-8 py-10 lg:grid-cols-[minmax(0,1.05fr)_420px] lg:items-center">
        <div className="max-w-3xl space-y-3">
          <h1 className="max-w-3xl text-4xl font-semibold leading-tight text-foreground sm:text-5xl">
            {msg("auth.login.title", "Build with Open Managed Agents")}
          </h1>
          <p className="max-w-2xl text-base leading-7 text-muted-foreground sm:text-lg">
            {msg(
              "auth.login.valueProp",
              "Create agents and applications with frontier models, durable sessions, and managed agent infrastructure.",
            )}
          </p>
        </div>

        {step === "email" ? (
          <form onSubmit={handleEmailSubmit}>
            <Card className="mx-auto w-full max-w-[420px]">
              <CardHeader className="space-y-2">
                <CardTitle>{msg("auth.login.continueWithEmail", "Continue with email")}</CardTitle>
                <CardDescription>
                  {msg(
                    "auth.login.emailStepDescription",
                    "Enter the email address associated with your Open Managed Agents account.",
                  )}
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-5">
                <Field className="gap-2">
                  <FieldLabel htmlFor="login-email">
                    {msg("auth.login.email", "Email")} <span aria-hidden="true">*</span>
                  </FieldLabel>
                  <Input
                    id="login-email"
                    autoComplete="email"
                    className="h-11"
                    inputMode="email"
                    onChange={(event) => setEmail(event.target.value)}
                    placeholder={msg("auth.login.emailPlaceholder", "name@company.com")}
                    type="email"
                    value={email}
                  />
                  <FieldDescription>
                    {msg("auth.login.emailHelp", "We'll send a one-time 6-digit verification code.")}
                  </FieldDescription>
                </Field>

                <StatusMessage error={error} notice={notice} />
              </CardContent>
              <CardFooter className="flex-col gap-3 border-0 bg-transparent pt-0">
                <Button className="h-10 w-full" disabled={isSubmitting} type="submit">
                  {isSubmitting ? <Loader2 className="mr-2 size-4 animate-spin" aria-hidden="true" /> : null}
                  {msg("auth.login.continueWithEmail", "Continue with email")}
                </Button>
              </CardFooter>
            </Card>
          </form>
        ) : (
          <form onSubmit={handleCodeSubmit}>
            <Card className="mx-auto w-full max-w-[420px]">
              <CardHeader className="space-y-2">
                <CardAction>
                  <Button
                    onClick={() => {
                      setStep("email");
                      setCode("");
                      setError(null);
                      setNotice(null);
                    }}
                    size="sm"
                    type="button"
                    variant="ghost"
                  >
                    <ArrowLeft className="size-4" aria-hidden="true" />
                    {msg("auth.login.changeEmail", "Change email")}
                  </Button>
                </CardAction>
                <CardTitle>{msg("auth.login.verifyEmail", "Verify email")}</CardTitle>
                <CardDescription>
                  {msg(
                    "auth.login.codeStepDescription",
                    "Use the verification code from your inbox to finish signing in.",
                  )}
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-5">
                <Field className="gap-2">
                  <FieldLabel htmlFor="login-code">
                    {msg("auth.login.verificationCode", "Verification code")}
                  </FieldLabel>
                  <FieldDescription>
                    {msg("auth.login.codeSentTo", "We sent a 6-digit code to {email}.", {
                      email: submittedEmail,
                    })}
                  </FieldDescription>
                  <Input
                    id="login-code"
                    autoComplete="one-time-code"
                    className="h-12 text-center text-xl font-semibold tracking-[0.32em]"
                    inputMode="numeric"
                    maxLength={6}
                    onChange={(event) => setCode(event.target.value.replace(/\D/g, "").slice(0, 6))}
                    placeholder="000000"
                    value={code}
                  />
                </Field>

                <StatusMessage error={error} notice={notice} />
              </CardContent>
              <CardFooter className="flex-col gap-2 border-0 bg-transparent pt-0">
                <Button className="h-10 w-full" disabled={isSubmitting} type="submit">
                  {isSubmitting ? <Loader2 className="mr-2 size-4 animate-spin" aria-hidden="true" /> : null}
                  {msg("auth.login.verifyEmail", "Verify email")}
                </Button>
                <Button
                  className="w-full"
                  disabled={isResending}
                  onClick={handleResend}
                  size="lg"
                  type="button"
                  variant="ghost"
                >
                  {isResending ? msg("auth.login.sending", "Sending...") : msg("auth.login.resendCode", "Resend code")}
                </Button>
              </CardFooter>
            </Card>
          </form>
        )}
      </section>
    </LoginShell>
  );
}

function LoginShell({ children }: { children: ReactNode }) {
  const { msg } = useI18n();

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border bg-background/95 backdrop-blur-sm">
        <div className="mx-auto flex h-16 max-w-7xl items-center justify-between px-5 lg:px-8">
          <a className="inline-flex items-center gap-2 text-sm font-semibold tracking-tight text-foreground" href="/">
            <span>{msg("app.productName", "Open Managed Agents")}</span>
          </a>
          <nav className="hidden items-center gap-1 md:flex">
            {topNavItems.map((item) => (
              <ButtonLink key={item.id} href={item.href} rel="noreferrer" size="sm" target="_blank" variant="ghost">
                {msg(item.id, item.label)}
              </ButtonLink>
            ))}
          </nav>
        </div>
      </header>

      <main className="mx-auto min-h-[calc(100vh-4rem)] max-w-7xl px-5 lg:px-8">{children}</main>
    </div>
  );
}

function StatusMessage({ error, notice }: { error: string | null; notice: string | null }) {
  if (!error && !notice) {
    return null;
  }

  return (
    <Alert role={error ? "alert" : "status"} variant={error ? "destructive" : "default"}>
      {error ? <CircleAlert aria-hidden="true" /> : <MailCheck aria-hidden="true" />}
      <AlertDescription>{error ?? notice}</AlertDescription>
    </Alert>
  );
}

function isValidEmail(value: string) {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value);
}

function errorMessage(error: unknown, fallback: string) {
  if (error && typeof error === "object" && "message" in error) {
    const message = (error as { message?: unknown }).message;
    if (typeof message === "string" && message.trim() !== "") {
      return message;
    }
  }
  return fallback;
}
