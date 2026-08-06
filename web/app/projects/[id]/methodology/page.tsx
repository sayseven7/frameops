import { Workspace } from "../../../page";

type ProjectPageProps = { params: Promise<{ id: string }> };

export default async function ProjectMethodologyPage({ params }: ProjectPageProps) {
  const { id } = await params;
  return <Workspace initialSection="methodologies" initialProjectID={id} />;
}
