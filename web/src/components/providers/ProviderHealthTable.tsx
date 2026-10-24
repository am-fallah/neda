import type { ProviderInfo } from "@/api/types";
import { ProviderHealthBadge, TestOnlyBadge } from "@/components/status";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatTehranTimestamp } from "@/lib/dates";

export function ProviderHealthTable({
  providers,
}: {
  providers: ProviderInfo[];
}) {
  if (providers.length === 0) {
    return (
      <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
        No ticket providers are registered in this build.
      </p>
    );
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
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium">{provider.displayName}</span>
                {provider.testOnly ? <TestOnlyBadge /> : null}
              </div>
              <p className="mt-0.5 font-mono text-xs text-muted-foreground">
                {provider.id}
              </p>
            </TableCell>
            <TableCell>
              <ProviderHealthBadge provider={provider} />
              {provider.health?.openEpisode ? (
                <p className="mt-1 text-xs text-muted-foreground">
                  Since{" "}
                  {formatTehranTimestamp(provider.health.openEpisode.openedAt)}
                </p>
              ) : null}
            </TableCell>
            <TableCell className="whitespace-nowrap text-muted-foreground">
              {formatTehranTimestamp(provider.health?.lastSuccessAt)}
            </TableCell>
            <TableCell className="whitespace-nowrap text-muted-foreground">
              {formatTehranTimestamp(provider.health?.lastAttemptAt)}
            </TableCell>
            <TableCell className="text-muted-foreground">
              {provider.health?.lastErrorKind ? (
                <span className="font-mono text-xs">
                  {provider.health.lastErrorKind}
                </span>
              ) : (
                "—"
              )}
            </TableCell>
            <TableCell className="tabular-nums">
              {provider.health?.consecutiveFailures ?? 0}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
