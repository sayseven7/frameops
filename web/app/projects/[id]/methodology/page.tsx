import ProtectedWorkspace from "../../../protected-workspace";

type ProjectPageProps = { params: Promise<{ id: string }> };

export default async function ProjectMethodologyPage({ params }: ProjectPageProps) {
  const { id } = await params;
  return <ProtectedWorkspace initialSection="methodologies" initialProjectID={id} />;
}
