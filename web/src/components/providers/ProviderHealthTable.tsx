import type { ProviderInfo } from "@/api/types";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import { ProviderHealthBadge, TestOnlyBadge } from "@/components/status";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatTehranTimestamp } from "@/lib/dates";

function ProviderIdentity({ provider }: { provider: ProviderInfo }) {
  return (
    <div className="min-w-0">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium break-words">{provider.displayName}</span>
        {provider.testOnly ? <TestOnlyBadge /> : null}
      </div>
      <p className="mt-0.5 font-mono text-[0.65rem] break-all text-muted-foreground">
        {provider.id}
      </p>
    </div>
  );
}

function ProviderHealthDetail({ provider }: { provider: ProviderInfo }) {
  const health = provider.health;
  return (
    <>
      <ProviderHealthBadge provider={provider} />
      {health?.openEpisode ? (
        <p className="mt-1 font-mono text-[0.65rem] text-muted-foreground">
          Since {formatTehranTimestamp(health.openEpisode.openedAt)}
        </p>
      ) : null}
    </>
  );
}

function ProviderCards({ providers }: { providers: ProviderInfo[] }) {
  return (
    <ul className="space-y-2" aria-label="Provider health">
      {providers.map((provider) => {
        const health = provider.health;
        return (
          <li
            key={provider.id}
            className="rounded-lg border bg-surface-raised p-3.5"
          >
            <div className="flex flex-wrap items-start justify-between gap-2">
              <ProviderIdentity provider={provider} />
              <ProviderHealthDetail provider={provider} />
            </div>
            <dl className="mt-3 grid grid-cols-2 gap-x-3 gap-y-2 border-t pt-3 text-xs">
              <div className="min-w-0">
                <dt className="eyebrow text-[0.58rem]">Last success</dt>
                <dd className="mt-0.5 font-mono text-[0.7rem] text-muted-foreground">
                  {formatTehranTimestamp(health?.lastSuccessAt)}
                </dd>
              </div>
              <div className="min-w-0">
                <dt className="eyebrow text-[0.58rem]">Last attempt</dt>
                <dd className="mt-0.5 font-mono text-[0.7rem] text-muted-foreground">
                  {formatTehranTimestamp(health?.lastAttemptAt)}
                </dd>
              </div>
              <div className="min-w-0">
                <dt className="eyebrow text-[0.58rem]">Last error</dt>
                <dd className="mt-0.5 font-mono text-[0.7rem] break-all text-muted-foreground">
                  {health?.lastErrorKind || "—"}
                </dd>
              </div>
              <div>
                <dt className="eyebrow text-[0.58rem]">Failures</dt>
                <dd className="mt-0.5 flex items-center gap-2">
                  <span className="font-mono text-[0.7rem] tabular-nums">
                    {health?.consecutiveFailures ?? 0}
                  </span>
                  {health?.stale ? <Badge variant="warning">Stale</Badge> : null}
                </dd>
              </div>
            </dl>
          </li>
        );
      })}
    </ul>
  );
}

export function ProviderHealthTable({
  providers,
}: {
  providers: ProviderInfo[];
}) {
  const wide = useMediaQuery("(min-width: 1100px)");
  if (providers.length === 0) {
    return (
      <p className="rounded-lg border border-dashed bg-surface-raised p-5 text-center text-sm text-muted-foreground">
        No ticket providers are registered in this build.
      </p>
    );
  }
  if (!wide) {
    return <ProviderCards providers={providers} />;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Provider</TableHead>
          <TableHead>Health</TableHead>
          <TableHead>Last success</TableHead>
          <TableHead>Last attempt</TableHead>
          <TableHead>Last error</TableHead>
          <TableHead>Failures</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {providers.map((provider) => (
          <TableRow key={provider.id}>
            <TableCell>
              <ProviderIdentity provider={provider} />
            </TableCell>
            <TableCell>
              <ProviderHealthDetail provider={provider} />
            </TableCell>
            <TableCell className="whitespace-nowrap font-mono text-[0.7rem] text-muted-foreground">
              {formatTehranTimestamp(provider.health?.lastSuccessAt)}
            </TableCell>
            <TableCell className="whitespace-nowrap font-mono text-[0.7rem] text-muted-foreground">
              {formatTehranTimestamp(provider.health?.lastAttemptAt)}
            </TableCell>
            <TableCell className="text-muted-foreground">
              {provider.health?.lastErrorKind ? (
                <span className="font-mono text-[0.7rem]">
                  {provider.health.lastErrorKind}
                </span>
              ) : (
                "—"
              )}
            </TableCell>
            <TableCell className="font-mono tabular-nums">
              {provider.health?.consecutiveFailures ?? 0}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
