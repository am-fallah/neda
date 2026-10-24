import { ExternalLink } from "lucide-react";
import { safeExternalUrl } from "@/lib/utils";

export function BookingLink({
  url,
  compact = false,
}: {
  url?: string | null;
  compact?: boolean;
}) {
  const safe = safeExternalUrl(url);
  if (!safe) {
    return (
      <span className="text-xs text-muted-foreground">
        No validated booking link
      </span>
    );
  }
  return (
    <a
      href={safe}
      target="_blank"
      rel="noreferrer noopener"
      className="inline-flex items-center gap-1 text-sm font-medium text-primary underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
    >
      {compact ? "Book" : "Open booking page"}
      <ExternalLink className="size-3.5" aria-hidden="true" />
      <span className="sr-only">(opens in a new tab)</span>
    </a>
  );
}
