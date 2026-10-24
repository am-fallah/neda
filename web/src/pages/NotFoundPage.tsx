import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/layout/PageHeader";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";

export function NotFoundPage() {
  useDocumentTitle("Not found");
  return (
    <div className="space-y-6">
      <PageHeader
        title="Page not found"
        description="That page does not exist in the dashboard."
      />
      <Button asChild variant="outline">
        <Link to="/">Back to overview</Link>
      </Button>
    </div>
  );
}
