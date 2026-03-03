#!/bin/bash

# Exit on error, undefined variables, and pipe failures
set -euo pipefail

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Logging functions
log_info() { echo -e "${GREEN}[INFO]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }

log_info "Starting Heimdall Infrastructure Bootstrap"
log_info "This script will provision the Terraform state backend and migrate the local state to AWS."
echo ""

# -----------------------------------------------------------------------------
# 0. Prerequisites Check
# -----------------------------------------------------------------------------
log_info "Checking prerequisites..."

if ! command -v terraform &> /dev/null; then
    log_error "Terraform not found. Please install Terraform v1.0+"
    exit 1
fi

# Verify Terraform version
TERRAFORM_VERSION=$(terraform version | grep Terraform | awk '{print $2}' | sed 's/v//')
if [[ $(echo "$TERRAFORM_VERSION" | cut -d. -f1) -lt 1 ]]; then
    log_error "Terraform version must be 1.0 or higher. Found: $TERRAFORM_VERSION"
    exit 1
fi
log_info "Terraform version verified: $TERRAFORM_VERSION"

if ! command -v aws &> /dev/null; then
    log_error "AWS CLI not found. Please install AWS CLI v2+"
    exit 1
fi

# Verify AWS credentials
if ! aws sts get-caller-identity &> /dev/null; then
    log_error "AWS credentials not configured or invalid. Run 'aws configure' first."
    exit 1
fi

log_info "All prerequisites met: Terraform, AWS CLI, and credentials verified"
echo ""

# Read AWS region with validation
AWS_VALID_REGIONS=(
    # US
    us-east-1 us-east-2 us-west-1 us-west-2
    # Africa
    af-south-1
    # Asia Pacific
    ap-east-1 ap-east-2 ap-south-1 ap-south-2 ap-northeast-1 ap-northeast-2 ap-northeast-3
    ap-southeast-1 ap-southeast-2 ap-southeast-3 ap-southeast-4 ap-southeast-5 ap-southeast-6 ap-southeast-7
    # Canada
    ca-central-1 ca-west-1
    # Europe
    eu-central-1 eu-central-2 eu-north-1 eu-south-1 eu-south-2 eu-west-1 eu-west-2 eu-west-3
    # Israel
    il-central-1
    # Mexico
    mx-central-1
    # Middle East
    me-central-1 me-south-1
    # South America
    sa-east-1
)
AWS_REGION="us-east-1"

while true; do
    read -r -p "Enter AWS region (default: us-east-1): " REGION_INPUT
    AWS_REGION="${REGION_INPUT:-us-east-1}"
    
    if [[ " ${AWS_VALID_REGIONS[@]} " =~ " ${AWS_REGION} " ]]; then
        log_info "Using AWS region: $AWS_REGION"
        break
    else
        log_error "Invalid AWS region: $AWS_REGION"
        echo "Valid regions:"
        printf '  %s\n' "${AWS_VALID_REGIONS[@]}" | sort
    fi
done
echo ""

# -----------------------------------------------------------------------------
# 1. Interactive Input with Regex Validation
# -----------------------------------------------------------------------------
SUFFIX_REGEX="^[a-z0-9-]+$"

while true; do
    read -r -p "Enter a unique suffix for the S3 state bucket (e.g., your initials or company prefix): " INPUT_SUFFIX
    
    if [[ -z "$INPUT_SUFFIX" ]]; then
        log_error "Suffix cannot be empty."
    elif [[ ! "$INPUT_SUFFIX" =~ $SUFFIX_REGEX ]]; then
        log_error "Invalid format. Suffix must contain only lowercase letters, numbers, and hyphens."
    else
        BUCKET_SUFFIX="$INPUT_SUFFIX"
        break
    fi
done

# Check for idempotency: if state already exists in S3, skip bootstrap
if aws s3api head-bucket --bucket "heimdall-tfstate-production-$BUCKET_SUFFIX" 2>/dev/null; then
    log_warn "S3 bucket already exists: heimdall-tfstate-production-$BUCKET_SUFFIX"
    read -r -p "Continue anyway? (y/n): " CONTINUE_INPUT
    if [[ ! "$CONTINUE_INPUT" =~ ^[yY]$ ]]; then
        log_info "Bootstrap cancelled."
        exit 0
    fi
fi
echo ""

# -----------------------------------------------------------------------------
# 2. Preparation
# -----------------------------------------------------------------------------
log_info "Cleaning up any existing backend configurations..."
rm -f backend.tf
log_info "Initializing Terraform (using local state)..."
terraform init -input=false

# -----------------------------------------------------------------------------
# 3. Execution
# -----------------------------------------------------------------------------
log_info "Applying AWS infrastructure (S3 Bucket and DynamoDB Table)..."
terraform apply -auto-approve \
  -var="bucket_name_suffix=$BUCKET_SUFFIX" \
  -var="aws_region=$AWS_REGION"

# -----------------------------------------------------------------------------
# 4. Extracting Outputs
# -----------------------------------------------------------------------------
log_info "Extracting provisioned resource identifiers..."

# Validate outputs exist and extract them
if ! terraform output state_bucket_name &>/dev/null; then
    log_error "Terraform output 'state_bucket_name' not found. Check outputs.tf defines this value."
    exit 1
fi

if ! terraform output lock_table_name &>/dev/null; then
    log_error "Terraform output 'lock_table_name' not found. Check outputs.tf defines this value."
    exit 1
fi

BUCKET_NAME=$(terraform output -raw state_bucket_name)
TABLE_NAME=$(terraform output -raw lock_table_name)

if [[ -z "$BUCKET_NAME" || -z "$TABLE_NAME" ]]; then
    log_error "Bucket or table name is empty. Bootstrap failed."
    exit 1
fi

log_info "Successfully provisioned Bucket: $BUCKET_NAME"
log_info "Successfully provisioned Table: $TABLE_NAME"
echo ""

# -----------------------------------------------------------------------------
# 5. State Backup (Before Migration)
# -----------------------------------------------------------------------------
log_info "Backing up local state files before migration..."
BACKUP_DIR=".backups"
mkdir -p "$BACKUP_DIR"

BACKUP_TIMESTAMP=$(date +%Y%m%d-%H%M%S)
if [[ -f terraform.tfstate ]]; then
    cp terraform.tfstate "$BACKUP_DIR/terraform.tfstate.$BACKUP_TIMESTAMP"
    log_info "Backed up: $BACKUP_DIR/terraform.tfstate.$BACKUP_TIMESTAMP"
fi

if [[ -f terraform.tfstate.backup ]]; then
    cp terraform.tfstate.backup "$BACKUP_DIR/terraform.tfstate.backup.$BACKUP_TIMESTAMP"
    log_info "Backed up: $BACKUP_DIR/terraform.tfstate.backup.$BACKUP_TIMESTAMP"
fi

if [[ -f .terraform.lock.hcl ]]; then
    cp .terraform.lock.hcl "$BACKUP_DIR/.terraform.lock.hcl.$BACKUP_TIMESTAMP"
    log_info "Backed up: $BACKUP_DIR/.terraform.lock.hcl.$BACKUP_TIMESTAMP"
fi
echo ""

# -----------------------------------------------------------------------------
# 6. State Migration
# -----------------------------------------------------------------------------
log_info "Migrating local state to the newly created S3 backend..."

# Create a backend.tf file with the backend configuration
log_info "Creating backend.tf with S3 configuration..."
cat > backend.tf <<EOF
terraform {
  backend "s3" {
    bucket         = "$BUCKET_NAME"
    key            = "bootstrap/terraform.tfstate"
    region         = "$AWS_REGION"
    encrypt        = true
    dynamodb_table = "$TABLE_NAME"
  }
}
EOF

log_info "Reinitializing Terraform with S3 backend..."
if ! echo "yes" | terraform init -migrate-state; then
    log_error "State migration failed."
    log_warn "Your state has been backed up to: $BACKUP_DIR/"
    log_warn "To rollback, restore from backup and remove backend.tf"
    rm -f backend.tf
    exit 1
fi

# Verify state migration was successful
log_info "Verifying state migration to S3..."
if ! aws s3api head-object --bucket "$BUCKET_NAME" --key "bootstrap/terraform.tfstate" &>/dev/null; then
    log_error "Failed to verify state file in S3 bucket."
    log_warn "State may not have been properly migrated. Check manually."
    exit 1
fi
log_info "State migration verified successfully in S3"
echo ""

# -----------------------------------------------------------------------------
# 7. Cleanup
# -----------------------------------------------------------------------------
log_info "Removing sensitive local state files..."
rm -f terraform.tfstate terraform.tfstate.backup
log_info "Local state files removed (backups preserved in $BACKUP_DIR/)"
echo ""

log_info "Bootstrap completed successfully!"
log_info "Infrastructure state is now securely managed in AWS S3"
log_info "State backups available at: $BACKUP_DIR/"
echo ""
echo "=========================================================================="
echo "Next Steps:"
echo "1. Create layer directories (e.g., layer1-network, layer2-eks)"
echo "2. In each layer, create a backend.tf declaring ONLY the key:"
echo ""
echo "   terraform {"
echo "     backend \"s3\" {"
echo "       key = \"layer<N>/terraform.tfstate\""
echo "     }"
echo "   }"
echo ""
echo "3. Configure your CI/CD to dynamically generate a backend.hcl containing:"
echo "   bucket         = \"$BUCKET_NAME\""
echo "   region         = \"$AWS_REGION\""
echo "   encrypt        = true"
echo "   dynamodb_table = \"$TABLE_NAME\""
echo ""
echo "4. Run 'terraform init -backend-config=backend.hcl' in the CI pipeline."
echo "=========================================================================="
