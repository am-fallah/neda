import { ArrowLeft, Compass, Plus } from "lucide-react";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";

export function NotFoundPage() {
  useDocumentTitle("Not found");
  return (
    <div className="flex min-h-[60dvh] items-center justify-center py-8">
      <div className="w-full max-w-xl rounded-[10px] border bg-card p-6 shadow-xs sm:p-8">
        <p className="eyebrow flex items-center gap-2">
          <span className="brand-rule h-px w-8" aria-hidden="true" />
          404 · Off the timetable
        </p>
        <h1 className="mt-3 text-[1.75rem] font-semibold tracking-[-0.025em] text-balance">
          Page not found
        </h1>
        <p className="mt-2 max-w-md text-sm leading-relaxed text-muted-foreground">
          That page does not exist in the dashboard. It may have been removed,
          or the link may point to a monitor that no longer exists.
        </p>
        <div className="mt-5 flex flex-wrap gap-2">
          <Button asChild>
            <Link to="/">
              <ArrowLeft aria-hidden="true" />
              Back to overview
            </Link>
          </Button>
          <Button asChild variant="outline">
            <Link to="/monitors">
              <Compass aria-hidden="true" />
              Browse monitors
            </Link>
          </Button>
          <Button asChild variant="ghost">
            <Link to="/monitors/new">
              <Plus aria-hidden="true" />
              New monitor
            </Link>
          </Button>
        </div>
      </div>
    </div>
  );
}
