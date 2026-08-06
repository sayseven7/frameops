import ProtectedWorkspace from "../../../protected-workspace";

type ProjectPageProps = { params: Promise<{ id: string }> };

export default async function ProjectReportsPage({ params }: ProjectPageProps) {
  const { id } = await params;
  return <ProtectedWorkspace initialSection="reports" initialProjectID={id} />;
}
