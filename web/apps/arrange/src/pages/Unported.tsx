import { Card, Page, PageHeader } from "../ui/index.ts";
import { consoleHref } from "../router.ts";
import { useT } from "../i18n.tsx";

/**
 * A page not yet rebuilt here. It links to the same page in the classic
 * console, so the app works throughout the migration.
 */
export function Unported({ title, classic }: { title: string; classic: string }) {
  const t = useT();
  return (
    <Page>
      <PageHeader title={t.t(title)} />
      <Card>
        <p>{t.t("This page is still in the classic console.")}</p>
        <p>
          <a className="btn primary" href={consoleHref(classic)}>{t.t("Open it there")}</a>
        </p>
      </Card>
    </Page>
  );
}
