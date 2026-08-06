import { Workspace } from "../../../page";

type ProjectPageProps = { params: Promise<{ id: string }> };

export default async function ProjectReportsPage({ params }: ProjectPageProps) {
  const { id } = await params;
  return <Workspace initialSection="reports" initialProjectID={id} />;
}
